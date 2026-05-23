package browser

import (
	"github.com/mbbennis/s3lf/internal/cache"
	"github.com/mbbennis/s3lf/internal/s3fs"
)

// MoveCursor adjusts the current pane's cursor by delta, clamped to the
// loaded entry count. The cache lookup may miss (listing not yet fetched),
// in which case we still update the cursor but clamp to >=0; the TUI will
// re-clamp after the listing arrives.
func (m *Model) MoveCursor(c *cache.Cache, delta int) {
	cur := m.Current()
	n := loadedCount(c, cur.Path)
	cur.Cursor += delta
	if cur.Cursor < 0 {
		cur.Cursor = 0
	}
	if n > 0 && cur.Cursor >= n {
		cur.Cursor = n - 1
	}
}

// GotoTop / GotoBottom: gg / G.
func (m *Model) GotoTop()                    { m.Current().Cursor = 0 }
func (m *Model) GotoBottom(c *cache.Cache)   { m.Current().Cursor = lastIndex(c, m.Current().Path) }

// Enter descends into the entry under the cursor (a directory or bucket).
// Returns the new Current path and true; false if the cursor is on a file
// or the listing isn't loaded.
func (m *Model) Enter(c *cache.Cache) (s3fs.Path, bool) {
	next, ok := m.PreviewPath(c)
	if !ok {
		return s3fs.Path{}, false
	}
	m.Stack = append(m.Stack, Pane{Path: next, Cursor: 0})
	return next, true
}

// Back pops the stack and returns to the parent pane (preserving its cursor).
func (m *Model) Back() bool {
	if len(m.Stack) <= 1 {
		return false
	}
	m.Stack = m.Stack[:len(m.Stack)-1]
	return true
}

func loadedCount(c *cache.Cache, p s3fs.Path) int {
	if p.IsRoot() {
		bs, ok := c.PeekBuckets()
		if !ok {
			return 0
		}
		return len(bs)
	}
	l, ok := c.Get(cache.Key{Bucket: p.Bucket, Prefix: p.Prefix})
	if !ok {
		return 0
	}
	return len(l.Entries)
}

func lastIndex(c *cache.Cache, p s3fs.Path) int {
	n := loadedCount(c, p)
	if n == 0 {
		return 0
	}
	return n - 1
}
