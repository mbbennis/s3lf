package tui

import (
	"strings"
	"unicode"

	"github.com/mbbennis/s3lf/internal/cache"
)

// smartcaseContains: case-insensitive unless needle contains uppercase,
// matching vim/lf 'smartcase'.
func smartcaseContains(haystack, needle string) bool {
	if needle == "" {
		return false
	}
	hasUpper := false
	for _, r := range needle {
		if unicode.IsUpper(r) {
			hasUpper = true
			break
		}
	}
	if hasUpper {
		return strings.Contains(haystack, needle)
	}
	return strings.Contains(strings.ToLower(haystack), strings.ToLower(needle))
}

// findMatch returns the index of the next/prev entry name in the current
// pane matching query, starting the scan at start+direction (i.e. exclusive
// of start). Wraps once. Returns (-1, false) if no match.
func (a *App) findMatch(query string, start int, forward bool) (int, bool) {
	if query == "" {
		return -1, false
	}
	cur := a.model.Current()
	var names []string
	if cur.Path.IsRoot() {
		bs, ok := a.cache.PeekBuckets()
		if !ok {
			return -1, false
		}
		names = make([]string, len(bs))
		for i, b := range bs {
			names[i] = b.Name
		}
	} else {
		l, ok := a.cache.Get(cache.Key{Bucket: cur.Path.Bucket, Prefix: cur.Path.Prefix})
		if !ok {
			return -1, false
		}
		names = make([]string, len(l.Entries))
		for i, e := range l.Entries {
			names[i] = e.Name
		}
	}
	n := len(names)
	if n == 0 {
		return -1, false
	}

	step := 1
	if !forward {
		step = -1
	}
	// Two passes: from start+step to end (or 0), then wrap.
	i := start + step
	for count := 0; count < n; count++ {
		if i < 0 {
			i = n - 1
		} else if i >= n {
			i = 0
		}
		if smartcaseContains(names[i], query) {
			return i, true
		}
		i += step
	}
	return -1, false
}
