package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	tea "github.com/charmbracelet/bubbletea"
)

// picker is a modal list-selector. Generic so future call sites (allowed
// buckets, recent prefixes, …) can reuse it. While App.picker != nil, all
// keystrokes route here.
type picker struct {
	label    string
	items    []string
	cursor   int
	onSelect func(string) tea.Cmd
	onCancel func() tea.Cmd
}

func (p *picker) move(delta int) {
	p.cursor += delta
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor >= len(p.items) {
		p.cursor = len(p.items) - 1
	}
}

func (p *picker) current() (string, bool) {
	if p.cursor < 0 || p.cursor >= len(p.items) {
		return "", false
	}
	return p.items[p.cursor], true
}

func (p *picker) render() string {
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true)
	cursor := lipgloss.NewStyle().Reverse(true)

	var b strings.Builder
	b.WriteString(titleStyle.Render(p.label))
	b.WriteString("\n\n")
	for i, item := range p.items {
		line := "  " + item
		if i == p.cursor {
			line = cursor.Render("▸ " + item)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("j/k move · enter select · esc cancel"))
	return b.String()
}

// handlePickerKey is the modal handler used while App.picker is non-nil.
func (a *App) handlePickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := a.picker
	switch {
	case matchKey(msg, "esc", "ctrl+c", "q"):
		a.picker = nil
		if p.onCancel != nil {
			return a, p.onCancel()
		}
	case matchKey(msg, "j", "down"):
		p.move(1)
	case matchKey(msg, "k", "up"):
		p.move(-1)
	case matchKey(msg, "enter", "l"):
		if v, ok := p.current(); ok {
			a.picker = nil
			if p.onSelect != nil {
				return a, p.onSelect(v)
			}
		}
	}
	return a, nil
}
