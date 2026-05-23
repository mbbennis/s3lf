package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/mbbennis/s3lf/internal/browser"
	"github.com/mbbennis/s3lf/internal/cache"
	"github.com/mbbennis/s3lf/internal/s3fs"
)

var (
	dirStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	cursorStyle   = lipgloss.NewStyle().Reverse(true)
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	headerStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	colBorder     = lipgloss.NewStyle().BorderStyle(lipgloss.NormalBorder()).BorderRight(true).BorderForeground(lipgloss.Color("238"))
)

func (a *App) View() string {
	if a.width == 0 || a.height == 0 {
		return "loading…"
	}
	if a.help {
		return a.helpView()
	}
	if a.picker != nil {
		return a.picker.render()
	}

	// Reserve 2 lines: header, statusbar. The columns get the rest.
	bodyHeight := a.height - 2
	if bodyHeight < 1 {
		bodyHeight = 1
	}

	// Column widths: 25 / 35 / 40 (last absorbs rounding).
	w1 := a.width * 25 / 100
	w2 := a.width * 35 / 100
	w3 := a.width - w1 - w2

	parentCol := a.renderParent(w1, bodyHeight)
	currentCol := a.renderCurrent(w2, bodyHeight)
	previewCol := a.renderPreview(w3, bodyHeight)

	body := lipgloss.JoinHorizontal(lipgloss.Top, parentCol, currentCol, previewCol)
	return lipgloss.JoinVertical(lipgloss.Left, a.header(), body, a.bottomLine())
}

// bottomLine renders the active prompt or the statusbar on the left, with
// the profile chip pinned to the bottom-right so it's always visible.
func (a *App) bottomLine() string {
	var left string
	if a.prompt != nil {
		left = a.prompt.render()
	} else {
		left = a.statusbar()
	}
	if a.profile == "" {
		return left
	}
	chip := chipStyle("14").Render(a.profile)
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(chip)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + chip
}

func (a *App) header() string {
	p := a.model.Current().Path
	left := headerStyle.Render(p.String())
	if !a.readOnly {
		return left
	}
	chip := chipStyle("11").Render("RO")
	gap := a.width - lipgloss.Width(left) - lipgloss.Width(chip)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + chip
}

func chipStyle(bg string) lipgloss.Style {
	return lipgloss.NewStyle().
		Foreground(lipgloss.Color("0")).
		Background(lipgloss.Color(bg)).
		Bold(true).
		Padding(0, 1)
}

func (a *App) statusbar() string {
	cur := a.model.Current()
	var parts []string

	if cur.Path.IsRoot() {
		if bs, ok := a.cache.PeekBuckets(); ok {
			parts = append(parts, fmt.Sprintf("%d buckets", len(bs)))
		}
	} else {
		k := cache.Key{Bucket: cur.Path.Bucket, Prefix: cur.Path.Prefix}
		if l, ok := a.cache.Get(k); ok {
			more := ""
			if !l.Complete {
				more = "+"
			}
			age := time.Since(l.FetchedAt).Truncate(time.Second)
			parts = append(parts, fmt.Sprintf("%d%s entries", len(l.Entries), more))
			parts = append(parts, fmt.Sprintf("age %s", age))
			if a.inflightNext[k] {
				parts = append(parts, "fetching more…")
			}
		} else {
			parts = append(parts, "loading…")
		}
	}

	if a.committed != "" {
		parts = append(parts, "/"+a.committed)
	}
	if a.status != "" {
		parts = append(parts, a.status)
	}
	return dimStyle.Render(strings.Join(parts, "  ·  "))
}

func (a *App) renderParent(w, h int) string {
	par := a.model.Parent()
	if par == nil {
		// At root — render an empty column so widths stay stable.
		return colBorder.Width(w).Height(h).Render("")
	}
	return colBorder.Width(w).Height(h).Render(a.renderListColumn(par, w, h, false))
}

func (a *App) renderCurrent(w, h int) string {
	cur := a.model.Current()
	return colBorder.Width(w).Height(h).Render(a.renderListColumn(cur, w, h, true))
}

func (a *App) renderPreview(w, h int) string {
	cur := a.model.Current()
	previewPath, ok := a.model.PreviewPath(a.cache)
	if ok {
		// Directory/bucket preview: render its listing read-only.
		previewPane := &browser.Pane{Path: previewPath, Cursor: -1}
		// Render the listing if loaded; otherwise placeholder.
		if previewPath.IsRoot() {
			return lipgloss.NewStyle().Width(w).Height(h).Render("")
		}
		k := cache.Key{Bucket: previewPath.Bucket, Prefix: previewPath.Prefix}
		if _, loaded := a.cache.Get(k); !loaded {
			return lipgloss.NewStyle().Width(w).Height(h).Render(dimStyle.Render("loading…"))
		}
		return lipgloss.NewStyle().Width(w).Height(h).Render(a.renderListColumn(previewPane, w, h, false))
	}

	// File preview: show metadata.
	if entry, ok := browser.VisibleEntry(a.cache, cur); ok && !entry.IsDir {
		return lipgloss.NewStyle().Width(w).Height(h).Render(fileMeta(entry))
	}
	return lipgloss.NewStyle().Width(w).Height(h).Render("")
}

// renderListColumn renders the entries of pane p into a single-column string.
// active=true draws the cursor row reverse-highlighted.
func (a *App) renderListColumn(p *browser.Pane, w, h int, active bool) string {
	rows := a.entriesFor(p)
	if rows == nil {
		return dimStyle.Render("loading…")
	}
	if len(rows) == 0 {
		return dimStyle.Render("(empty)")
	}

	// Scroll window: keep cursor in view.
	start := 0
	if p.Cursor >= h {
		start = p.Cursor - h + 1
	}
	end := start + h
	if end > len(rows) {
		end = len(rows)
	}

	var b strings.Builder
	for i := start; i < end; i++ {
		line := truncate(rows[i].render(w-1), w-1)
		if active && i == p.Cursor {
			line = cursorStyle.Render(padRight(line, w-1))
		}
		b.WriteString(line)
		if i < end-1 {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

type row struct {
	name  string
	isDir bool
	right string // right-aligned suffix (size, etc.)
}

func (r row) render(w int) string {
	name := r.name
	if r.isDir {
		name = dirStyle.Render(name + "/")
	}
	if r.right == "" {
		return name
	}
	// Pad name to leave room for the right column.
	rightLen := lipgloss.Width(r.right)
	nameLen := lipgloss.Width(name)
	pad := w - nameLen - rightLen
	if pad < 1 {
		pad = 1
	}
	return name + strings.Repeat(" ", pad) + dimStyle.Render(r.right)
}

func (a *App) entriesFor(p *browser.Pane) []row {
	if p.Path.IsRoot() {
		bs, ok := a.cache.PeekBuckets()
		if !ok {
			return nil
		}
		rows := make([]row, len(bs))
		for i, b := range bs {
			rows[i] = row{name: b.Name, isDir: true}
		}
		return rows
	}
	l, ok := a.cache.Get(cache.Key{Bucket: p.Path.Bucket, Prefix: p.Path.Prefix})
	if !ok {
		return nil
	}
	rows := make([]row, len(l.Entries))
	for i, e := range l.Entries {
		r := row{name: e.Name, isDir: e.IsDir}
		if !e.IsDir {
			r.right = humanSize(e.Size)
		}
		rows[i] = r
	}
	return rows
}

func fileMeta(e *s3fs.Entry) string {
	lines := []string{
		headerStyle.Render(e.Name),
		"",
		dimStyle.Render("size      ") + humanSize(e.Size),
		dimStyle.Render("modified  ") + e.Modified.Format(time.RFC3339),
	}
	if e.Storage != "" {
		lines = append(lines, dimStyle.Render("storage   ")+e.Storage)
	}
	return strings.Join(lines, "\n")
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(n)/float64(div), "KMGTPE"[exp])
}

func padRight(s string, w int) string {
	width := lipgloss.Width(s)
	if width >= w {
		return s
	}
	return s + strings.Repeat(" ", w-width)
}

func truncate(s string, w int) string {
	if lipgloss.Width(s) <= w {
		return s
	}
	// Simple rune-based truncation. lipgloss.Width handles ANSI; this loses
	// ANSI on overflow, which is acceptable for v1 (filenames don't carry
	// color across the boundary).
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > w {
		runes = runes[:len(runes)-1]
	}
	return string(runes)
}
