package browser

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/mbbennis/s3lf/internal/cache"
	"github.com/mbbennis/s3lf/internal/s3fs"
)

// stubFS is just enough FS for the cache to be populated by tests. The
// browser tests don't drive real fetches — they prime cache via Fetch /
// Buckets and then exercise the state machine.
type stubFS struct {
	buckets  []s3fs.Bucket
	listings map[string]*s3fs.Listing // key: bucket|prefix
}

func k(bucket, prefix string) string { return bucket + "|" + prefix }

func (s *stubFS) ListBuckets(ctx context.Context) ([]s3fs.Bucket, error) { return s.buckets, nil }
func (s *stubFS) List(ctx context.Context, bucket, prefix, token string) (*s3fs.Listing, error) {
	if l, ok := s.listings[k(bucket, prefix)]; ok {
		return l, nil
	}
	return &s3fs.Listing{Bucket: bucket, Prefix: prefix, Complete: true, FetchedAt: time.Now()}, nil
}
func (s *stubFS) Region(ctx context.Context, bucket string) (string, error) { return "us-east-1", nil }
func (s *stubFS) HeadObject(ctx context.Context, bucket, key string) (*s3fs.ObjectInfo, error) {
	return &s3fs.ObjectInfo{}, nil
}
func (s *stubFS) Download(ctx context.Context, bucket, key string, w io.WriterAt, ifMatch string) (int64, error) {
	return 0, nil
}
func (s *stubFS) Upload(ctx context.Context, bucket, key string, r io.Reader, size int64, ct, ifMatch string) error {
	return nil
}
func (s *stubFS) Delete(ctx context.Context, bucket, key string) error { return nil }

// build returns a model + cache primed with the given listings/buckets.
func build(t *testing.T, buckets []s3fs.Bucket, listings map[string]*s3fs.Listing) (*Model, *cache.Cache) {
	t.Helper()
	fs := &stubFS{buckets: buckets, listings: listings}
	c := cache.New(fs, 16, time.Minute)
	if len(buckets) > 0 {
		if _, err := c.Buckets(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	// Prime each listing into the cache via Fetch so cache.Get returns hits.
	for key := range listings {
		var bucket, prefix string
		for i := 0; i < len(key); i++ {
			if key[i] == '|' {
				bucket, prefix = key[:i], key[i+1:]
				break
			}
		}
		if _, err := c.Fetch(context.Background(), cache.Key{Bucket: bucket, Prefix: prefix}); err != nil {
			t.Fatal(err)
		}
	}
	m := New()
	return &m, c
}

func TestNewStartsAtRoot(t *testing.T) {
	m := New()
	if !m.Current().Path.IsRoot() {
		t.Errorf("expected new model to start at root, got %+v", m.Current().Path)
	}
	if m.Parent() != nil {
		t.Errorf("expected nil parent at root, got %+v", m.Parent())
	}
}

func TestEnterIntoBucketThenPrefix(t *testing.T) {
	buckets := []s3fs.Bucket{{Name: "foo"}, {Name: "bar"}}
	listings := map[string]*s3fs.Listing{
		k("foo", ""): {
			Bucket: "foo",
			Entries: []s3fs.Entry{
				{Name: "a", IsDir: true},
				{Name: "file.txt", Size: 12},
			},
			Complete: true, FetchedAt: time.Now(),
		},
		k("foo", "a/"): {
			Bucket: "foo", Prefix: "a/",
			Entries:  []s3fs.Entry{{Name: "nested", IsDir: true}},
			Complete: true, FetchedAt: time.Now(),
		},
	}
	m, c := build(t, buckets, listings)

	// Cursor on "foo"; Enter descends into the bucket.
	if _, ok := m.Enter(c); !ok {
		t.Fatal("Enter into bucket should succeed")
	}
	if m.Current().Path.Bucket != "foo" {
		t.Errorf("after Enter, bucket = %q, want foo", m.Current().Path.Bucket)
	}

	// Cursor 0 is "a/" (a dir). Enter again.
	if _, ok := m.Enter(c); !ok {
		t.Fatal("Enter into dir should succeed")
	}
	if m.Current().Path.Prefix != "a/" {
		t.Errorf("after Enter, prefix = %q, want a/", m.Current().Path.Prefix)
	}
}

func TestEnterOnFileFails(t *testing.T) {
	buckets := []s3fs.Bucket{{Name: "foo"}}
	listings := map[string]*s3fs.Listing{
		k("foo", ""): {
			Bucket: "foo",
			Entries: []s3fs.Entry{
				{Name: "file.txt", Size: 12}, // not a dir
			},
			Complete: true, FetchedAt: time.Now(),
		},
	}
	m, c := build(t, buckets, listings)
	if _, ok := m.Enter(c); !ok {
		t.Fatal("Enter into bucket should succeed")
	}
	// Cursor on "file.txt" — Enter must be a no-op.
	before := *m.Current()
	if _, ok := m.Enter(c); ok {
		t.Error("Enter on a file should return ok=false")
	}
	if *m.Current() != before {
		t.Error("Enter on a file must not mutate the stack")
	}
}

func TestBackPopsAndPreservesCursor(t *testing.T) {
	buckets := []s3fs.Bucket{{Name: "foo"}}
	listings := map[string]*s3fs.Listing{
		k("foo", ""): {
			Bucket:   "foo",
			Entries:  []s3fs.Entry{{Name: "a", IsDir: true}, {Name: "b", IsDir: true}},
			Complete: true, FetchedAt: time.Now(),
		},
		k("foo", "b/"): {
			Bucket: "foo", Prefix: "b/",
			Entries: []s3fs.Entry{{Name: "x"}}, Complete: true, FetchedAt: time.Now(),
		},
	}
	m, c := build(t, buckets, listings)
	m.Enter(c)              // into foo
	m.MoveCursor(c, 1)      // onto "b"
	m.Enter(c)              // into foo/b/
	if !m.Back() {
		t.Fatal("Back from foo/b should succeed")
	}
	if m.Current().Path.Prefix != "" || m.Current().Path.Bucket != "foo" {
		t.Errorf("expected to be back at s3://foo/, got %+v", m.Current().Path)
	}
	if m.Current().Cursor != 1 {
		t.Errorf("expected cursor preserved at 1, got %d", m.Current().Cursor)
	}
	if !m.Back() {
		t.Fatal("Back from foo should succeed")
	}
	if !m.Current().Path.IsRoot() {
		t.Errorf("expected root, got %+v", m.Current().Path)
	}
	if m.Back() {
		t.Error("Back at root should return false")
	}
}

func TestMoveCursorClamps(t *testing.T) {
	buckets := []s3fs.Bucket{{Name: "foo"}}
	listings := map[string]*s3fs.Listing{
		k("foo", ""): {
			Bucket:   "foo",
			Entries:  []s3fs.Entry{{Name: "a"}, {Name: "b"}, {Name: "c"}},
			Complete: true, FetchedAt: time.Now(),
		},
	}
	m, c := build(t, buckets, listings)
	m.Enter(c) // into foo
	m.MoveCursor(c, -5)
	if m.Current().Cursor != 0 {
		t.Errorf("MoveCursor below 0 left cursor at %d, want 0", m.Current().Cursor)
	}
	m.MoveCursor(c, 100)
	if m.Current().Cursor != 2 {
		t.Errorf("MoveCursor past end left cursor at %d, want 2", m.Current().Cursor)
	}
}

func TestGotoTopAndBottom(t *testing.T) {
	buckets := []s3fs.Bucket{{Name: "foo"}}
	listings := map[string]*s3fs.Listing{
		k("foo", ""): {
			Bucket:   "foo",
			Entries:  []s3fs.Entry{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}},
			Complete: true, FetchedAt: time.Now(),
		},
	}
	m, c := build(t, buckets, listings)
	m.Enter(c) // into foo
	m.MoveCursor(c, 2)
	m.GotoTop()
	if m.Current().Cursor != 0 {
		t.Errorf("GotoTop left cursor at %d, want 0", m.Current().Cursor)
	}
	m.GotoBottom(c)
	if m.Current().Cursor != 3 {
		t.Errorf("GotoBottom left cursor at %d, want 3", m.Current().Cursor)
	}
}

func TestPreviewPath(t *testing.T) {
	buckets := []s3fs.Bucket{{Name: "alpha"}, {Name: "beta"}}
	listings := map[string]*s3fs.Listing{
		k("alpha", ""): {
			Bucket: "alpha",
			Entries: []s3fs.Entry{
				{Name: "a", IsDir: true},
				{Name: "f.txt"},
			},
			Complete: true, FetchedAt: time.Now(),
		},
	}
	m, c := build(t, buckets, listings)

	// At root, preview should point to the bucket under the cursor.
	p, ok := m.PreviewPath(c)
	if !ok {
		t.Fatal("preview at root should be valid")
	}
	if p.Bucket != "alpha" || p.Prefix != "" {
		t.Errorf("preview at root = %+v, want bucket alpha", p)
	}

	// Inside alpha, cursor on the dir → preview is alpha/a/.
	m.Enter(c)
	p, ok = m.PreviewPath(c)
	if !ok {
		t.Fatal("preview should be valid on a dir")
	}
	if p.Prefix != "a/" {
		t.Errorf("preview prefix = %q, want a/", p.Prefix)
	}

	// Move cursor to the file — preview should be invalid.
	m.MoveCursor(c, 1)
	if _, ok := m.PreviewPath(c); ok {
		t.Error("preview on a file should be invalid")
	}
}
