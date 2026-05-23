package browser

import (
	"github.com/mbbennis/s3lf/internal/cache"
	"github.com/mbbennis/s3lf/internal/s3fs"
)

// Pane is one node on the navigation stack: a Path and the cursor position
// the user had there when they descended.
type Pane struct {
	Path   s3fs.Path
	Cursor int
}

// Model is the pure navigation state. It does NOT own listings — those live
// in the cache. View code asks the cache for the listing keyed by each
// visible Pane's Path, which keeps Model cheap to copy and easy to reason
// about.
//
// Invariant: Stack always contains at least one element. Stack[0] is the
// bucket-root pane (Path{}).
type Model struct {
	Stack []Pane
}

func New() Model {
	return Model{Stack: []Pane{{Path: s3fs.Path{}}}}
}

// Current is the rightmost main pane (the one whose cursor moves under j/k).
func (m *Model) Current() *Pane { return &m.Stack[len(m.Stack)-1] }

// Parent is the pane to the left of Current, or nil at the root.
func (m *Model) Parent() *Pane {
	if len(m.Stack) < 2 {
		return nil
	}
	return &m.Stack[len(m.Stack)-2]
}

// VisibleEntry returns the entry under the cursor in pane p, using the given
// cache. Returns (nil, false) if the listing isn't loaded yet or the cursor
// is out of bounds.
func VisibleEntry(c *cache.Cache, p *Pane) (*s3fs.Entry, bool) {
	if p == nil {
		return nil, false
	}
	if p.Path.IsRoot() {
		return nil, false
	}
	l, ok := c.Get(cache.Key{Bucket: p.Path.Bucket, Prefix: p.Path.Prefix})
	if !ok {
		return nil, false
	}
	if p.Cursor < 0 || p.Cursor >= len(l.Entries) {
		return nil, false
	}
	return &l.Entries[p.Cursor], true
}

// PreviewPath returns the Path the preview pane should render — the child
// of Current's cursor if that entry is a directory (or a bucket, on the
// root pane). Returns ok=false when there's nothing to preview (cursor on
// a file, or listing not loaded yet).
func (m *Model) PreviewPath(c *cache.Cache) (s3fs.Path, bool) {
	cur := m.Current()
	if cur.Path.IsRoot() {
		bs, ok := c.PeekBuckets()
		if !ok || cur.Cursor < 0 || cur.Cursor >= len(bs) {
			return s3fs.Path{}, false
		}
		return s3fs.Path{Bucket: bs[cur.Cursor].Name}, true
	}
	entry, ok := VisibleEntry(c, cur)
	if !ok || !entry.IsDir {
		return s3fs.Path{}, false
	}
	return cur.Path.Child(entry.Name), true
}
