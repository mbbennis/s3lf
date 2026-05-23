package cache

import (
	"context"
	"errors"
	"io"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mbbennis/s3lf/internal/s3fs"
)

// fakeFS is a minimal FS for cache tests. It records call counts so tests
// can assert dedupe / refetch behavior, and lets each test customize the
// listing returned for (bucket, prefix, token).
type fakeFS struct {
	listings    map[string]*s3fs.Listing // key: bucket|prefix|token
	listCalls   atomic.Int32
	bucketCalls atomic.Int32
	buckets     []s3fs.Bucket
	bucketsErr  error
	listErr     error
	delay       time.Duration
}

func key(bucket, prefix, token string) string {
	return bucket + "|" + prefix + "|" + token
}

func (f *fakeFS) ListBuckets(ctx context.Context) ([]s3fs.Bucket, error) {
	f.bucketCalls.Add(1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.bucketsErr != nil {
		return nil, f.bucketsErr
	}
	return f.buckets, nil
}

func (f *fakeFS) List(ctx context.Context, bucket, prefix, token string) (*s3fs.Listing, error) {
	f.listCalls.Add(1)
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.listErr != nil {
		return nil, f.listErr
	}
	if l, ok := f.listings[key(bucket, prefix, token)]; ok {
		return l, nil
	}
	return &s3fs.Listing{Bucket: bucket, Prefix: prefix, Complete: true, FetchedAt: time.Now()}, nil
}

// Unused-but-required methods for the interface.

func (f *fakeFS) Region(ctx context.Context, bucket string) (string, error) {
	return "us-east-1", nil
}
func (f *fakeFS) HeadObject(ctx context.Context, bucket, key string) (*s3fs.ObjectInfo, error) {
	return &s3fs.ObjectInfo{}, nil
}
func (f *fakeFS) Download(ctx context.Context, bucket, key string, w io.WriterAt, ifMatch string) (int64, error) {
	return 0, nil
}
func (f *fakeFS) Upload(ctx context.Context, bucket, key string, r io.Reader, size int64, ct, ifMatch string) error {
	return nil
}
func (f *fakeFS) Delete(ctx context.Context, bucket, key string) error { return nil }

func TestFetchAndGet(t *testing.T) {
	f := &fakeFS{
		listings: map[string]*s3fs.Listing{
			key("foo", "", ""): {
				Bucket: "foo", Prefix: "",
				Entries:  []s3fs.Entry{{Name: "a"}, {Name: "b"}},
				Complete: true, FetchedAt: time.Now(),
			},
		},
	}
	c := New(f, 8, time.Minute)
	k := Key{Bucket: "foo"}

	if _, ok := c.Get(k); ok {
		t.Fatal("expected miss before fetch")
	}
	l, err := c.Fetch(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 2 {
		t.Errorf("got %d entries, want 2", len(l.Entries))
	}
	if _, ok := c.Get(k); !ok {
		t.Fatal("expected hit after fetch")
	}
	// Second Fetch within TTL should not call the upstream FS again.
	if _, err := c.Fetch(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	if got := f.listCalls.Load(); got != 1 {
		t.Errorf("listCalls = %d, want 1", got)
	}
}

func TestFetchSingleflightDedupes(t *testing.T) {
	f := &fakeFS{delay: 50 * time.Millisecond}
	c := New(f, 8, time.Minute)
	k := Key{Bucket: "foo"}

	const n = 5
	done := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			_, _ = c.Fetch(context.Background(), k)
			done <- struct{}{}
		}()
	}
	for i := 0; i < n; i++ {
		<-done
	}
	if got := f.listCalls.Load(); got != 1 {
		t.Errorf("concurrent Fetch called upstream %d times, want 1", got)
	}
}

func TestTTLStaleness(t *testing.T) {
	f := &fakeFS{}
	c := New(f, 8, 10*time.Millisecond)
	k := Key{Bucket: "foo"}

	l, err := c.Fetch(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}
	if c.Stale(l) {
		t.Error("expected fresh listing not to be stale")
	}
	time.Sleep(20 * time.Millisecond)
	if !c.Stale(l) {
		t.Error("expected stale after TTL")
	}
}

func TestFetchNextAppends(t *testing.T) {
	f := &fakeFS{
		listings: map[string]*s3fs.Listing{
			key("foo", "", ""): {
				Bucket: "foo", Entries: []s3fs.Entry{{Name: "a"}},
				NextToken: "t1", Complete: false, FetchedAt: time.Now(),
			},
			key("foo", "", "t1"): {
				Bucket: "foo", Entries: []s3fs.Entry{{Name: "b"}, {Name: "c"}},
				Complete: true, FetchedAt: time.Now(),
			},
		},
	}
	c := New(f, 8, time.Minute)
	k := Key{Bucket: "foo"}

	if _, err := c.Fetch(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	l, err := c.FetchNext(context.Background(), k)
	if err != nil {
		t.Fatal(err)
	}
	if len(l.Entries) != 3 {
		t.Errorf("after FetchNext, %d entries, want 3", len(l.Entries))
	}
	if !l.Complete {
		t.Error("expected Complete=true after final page")
	}
	if got := f.listCalls.Load(); got != 2 {
		t.Errorf("listCalls = %d, want 2", got)
	}

	// FetchNext on a complete listing should be a no-op.
	if _, err := c.FetchNext(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	if got := f.listCalls.Load(); got != 2 {
		t.Errorf("listCalls after no-op FetchNext = %d, want 2", got)
	}
}

func TestInvalidate(t *testing.T) {
	f := &fakeFS{}
	c := New(f, 8, time.Minute)
	k := Key{Bucket: "foo"}
	if _, err := c.Fetch(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	c.Invalidate(k)
	if _, ok := c.Get(k); ok {
		t.Error("expected miss after Invalidate")
	}
	if _, err := c.Fetch(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	if got := f.listCalls.Load(); got != 2 {
		t.Errorf("listCalls after invalidate+fetch = %d, want 2", got)
	}
}

func TestLRUEviction(t *testing.T) {
	f := &fakeFS{}
	c := New(f, 2, time.Minute)
	keys := []Key{
		{Bucket: "a"}, {Bucket: "b"}, {Bucket: "c"},
	}
	for _, k := range keys {
		if _, err := c.Fetch(context.Background(), k); err != nil {
			t.Fatal(err)
		}
	}
	// 'a' was the LRU once 'c' arrived; should have been evicted.
	if _, ok := c.Get(keys[0]); ok {
		t.Error("expected oldest key to be evicted")
	}
	if _, ok := c.Get(keys[1]); !ok {
		t.Error("expected 'b' still present")
	}
	if _, ok := c.Get(keys[2]); !ok {
		t.Error("expected 'c' still present")
	}
}

func TestBucketsAndInvalidateBuckets(t *testing.T) {
	f := &fakeFS{buckets: []s3fs.Bucket{{Name: "alpha"}, {Name: "beta"}}}
	c := New(f, 8, time.Minute)

	if _, ok := c.PeekBuckets(); ok {
		t.Fatal("expected no cached buckets initially")
	}
	bs, err := c.Buckets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(bs) != 2 {
		t.Errorf("got %d buckets, want 2", len(bs))
	}
	if _, ok := c.PeekBuckets(); !ok {
		t.Fatal("expected cached buckets after fetch")
	}
	// Second Buckets within TTL: no extra call.
	if _, err := c.Buckets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.bucketCalls.Load(); got != 1 {
		t.Errorf("bucketCalls = %d, want 1", got)
	}

	c.InvalidateBuckets()
	if _, ok := c.PeekBuckets(); ok {
		t.Error("expected no cached buckets after InvalidateBuckets")
	}
	if _, err := c.Buckets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if got := f.bucketCalls.Load(); got != 2 {
		t.Errorf("bucketCalls after invalidate+refetch = %d, want 2", got)
	}
}

func TestFetchPropagatesError(t *testing.T) {
	sentinel := errors.New("boom")
	f := &fakeFS{listErr: sentinel}
	c := New(f, 8, time.Minute)
	if _, err := c.Fetch(context.Background(), Key{Bucket: "foo"}); !errors.Is(err, sentinel) {
		t.Errorf("Fetch error = %v, want wrapping %v", err, sentinel)
	}
}
