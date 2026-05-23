package cache

import (
	"container/list"
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/mbbennis/s3lf/internal/s3fs"
)

// Key identifies a cached listing. An empty Bucket means the bucket-root
// pane (s.Buckets is cached separately).
type Key struct {
	Bucket string
	Prefix string
}

// Cache wraps s3fs.FS with an LRU + singleflight. The TTL is advisory:
// stale entries are still returned but flagged so the UI can refresh.
type Cache struct {
	fs       s3fs.FS
	capacity int
	ttl      time.Duration

	mu      sync.Mutex
	entries map[Key]*list.Element
	order   *list.List

	buckets   []s3fs.Bucket
	bucketsAt time.Time

	sf singleflight.Group
}

type entry struct {
	key     Key
	listing *s3fs.Listing
}

func New(fs s3fs.FS, capacity int, ttl time.Duration) *Cache {
	return &Cache{
		fs:       fs,
		capacity: capacity,
		ttl:      ttl,
		entries:  make(map[Key]*list.Element, capacity),
		order:    list.New(),
	}
}

// Get returns the cached listing if present (regardless of TTL). The
// boolean indicates presence; the caller can inspect FetchedAt to decide
// on a background refresh.
func (c *Cache) Get(k Key) (*s3fs.Listing, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.entries[k]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	return el.Value.(*entry).listing, true
}

// Stale reports whether a listing is older than the TTL.
func (c *Cache) Stale(l *s3fs.Listing) bool {
	return time.Since(l.FetchedAt) > c.ttl
}

// Fetch returns the listing for k, calling out to s3fs on miss. Concurrent
// callers for the same key share one upstream call via singleflight.
func (c *Cache) Fetch(ctx context.Context, k Key) (*s3fs.Listing, error) {
	if l, ok := c.Get(k); ok && !c.Stale(l) {
		return l, nil
	}
	sfKey := k.Bucket + "\x00" + k.Prefix
	v, err, _ := c.sf.Do(sfKey, func() (any, error) {
		l, err := c.fs.List(ctx, k.Bucket, k.Prefix, "")
		if err != nil {
			return nil, err
		}
		c.put(k, l)
		return l, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*s3fs.Listing), nil
}

// PeekBuckets returns the cached bucket list without triggering a fetch.
func (c *Cache) PeekBuckets() ([]s3fs.Bucket, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.buckets == nil {
		return nil, false
	}
	return c.buckets, true
}

// Buckets returns the bucket list, fetching on first call or after TTL.
func (c *Cache) Buckets(ctx context.Context) ([]s3fs.Bucket, error) {
	c.mu.Lock()
	if c.buckets != nil && time.Since(c.bucketsAt) <= c.ttl {
		defer c.mu.Unlock()
		return c.buckets, nil
	}
	c.mu.Unlock()

	v, err, _ := c.sf.Do("__buckets__", func() (any, error) {
		bs, err := c.fs.ListBuckets(ctx)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		c.buckets = bs
		c.bucketsAt = time.Now()
		c.mu.Unlock()
		return bs, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]s3fs.Bucket), nil
}

// FetchNext appends the next page of entries to the cached listing for k.
// No-op (returns the existing listing) if the listing is already Complete
// or not yet in cache. Singleflight-keyed by the current NextToken so two
// concurrent scroll events for the same page dedupe; once the page lands
// and the token rotates, a follow-up call fires correctly.
func (c *Cache) FetchNext(ctx context.Context, k Key) (*s3fs.Listing, error) {
	cur, ok := c.Get(k)
	if !ok || cur.Complete || cur.NextToken == "" {
		return cur, nil
	}
	token := cur.NextToken
	sfKey := k.Bucket + "\x00" + k.Prefix + "\x00next\x00" + token

	v, err, _ := c.sf.Do(sfKey, func() (any, error) {
		page, err := c.fs.List(ctx, k.Bucket, k.Prefix, token)
		if err != nil {
			return nil, err
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		el, ok := c.entries[k]
		if !ok {
			// Evicted while we fetched — just cache the new page as-is.
			c.putLocked(k, page)
			return page, nil
		}
		existing := el.Value.(*entry).listing
		// Guard against a stale page from a prior token (e.g. user pressed R
		// during the in-flight call, replacing the listing).
		if existing.NextToken != token {
			return existing, nil
		}
		merged := &s3fs.Listing{
			Bucket:    existing.Bucket,
			Prefix:    existing.Prefix,
			Entries:   append(existing.Entries, page.Entries...),
			NextToken: page.NextToken,
			Complete:  page.NextToken == "",
			FetchedAt: existing.FetchedAt, // keep age of first page; UI cares about staleness from the user's view
		}
		el.Value.(*entry).listing = merged
		c.order.MoveToFront(el)
		return merged, nil
	})
	if err != nil {
		return nil, err
	}
	return v.(*s3fs.Listing), nil
}

// Invalidate drops a single key. Used by manual refresh (R).
func (c *Cache) Invalidate(k Key) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.entries[k]; ok {
		c.order.Remove(el)
		delete(c.entries, k)
	}
}

// InvalidateBuckets drops the cached bucket list so the next Buckets call
// refetches.
func (c *Cache) InvalidateBuckets() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buckets = nil
	c.bucketsAt = time.Time{}
}

func (c *Cache) put(k Key, l *s3fs.Listing) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.putLocked(k, l)
}

func (c *Cache) putLocked(k Key, l *s3fs.Listing) {
	if el, ok := c.entries[k]; ok {
		el.Value.(*entry).listing = l
		c.order.MoveToFront(el)
		return
	}
	el := c.order.PushFront(&entry{key: k, listing: l})
	c.entries[k] = el
	for c.order.Len() > c.capacity {
		back := c.order.Back()
		if back == nil {
			break
		}
		c.order.Remove(back)
		delete(c.entries, back.Value.(*entry).key)
	}
}
