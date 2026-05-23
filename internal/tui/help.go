package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// helpSection groups related bindings under a heading.
type helpSection struct {
	title    string
	bindings []helpBinding
}

type helpBinding struct {
	keys string
	desc string
}

// helpSections is the static source of truth. Kept here rather than derived
// from the keymap so descriptions are human-friendly (e.g. "go back" vs
// "h/left/backspace").
var helpSections = []helpSection{
	{
		title: "NAVIGATION",
		bindings: []helpBinding{
			{"j  ↓", "move down"},
			{"k  ↑", "move up"},
			{"l  →  enter", "enter directory / bucket"},
			{"h  ←  backspace", "go back"},
			{"gg", "go to top"},
			{"G", "go to bottom of loaded"},
			{"R", "refresh listing"},
		},
	},
	{
		title: "SEARCH",
		bindings: []helpBinding{
			{"/", "search (live, smartcase)"},
			{"n", "next match"},
			{"N", "previous match"},
			{"esc", "clear search"},
		},
	},
	{
		title: "FILE OPERATIONS",
		bindings: []helpBinding{
			{"y", "download to local"},
			{"v", "view in $PAGER"},
			{"e", "edit in $EDITOR"},
			{"o", "open with system default"},
			{"D", "delete (type filename to confirm)"},
		},
	},
	{
		title: "GENERAL",
		bindings: []helpBinding{
			{"P", "switch AWS profile"},
			{"?", "toggle this help"},
			{"q  ctrl-c", "quit"},
		},
	},
}

func (a *App) helpView() string {
	if a.width == 0 || a.height == 0 {
		return ""
	}

	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("12")).Bold(true)
	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")).Bold(true).Underline(true)

	// Find the widest key column so descriptions align.
	maxKey := 0
	for _, s := range helpSections {
		for _, b := range s.bindings {
			if w := lipgloss.Width(b.keys); w > maxKey {
				maxKey = w
			}
		}
	}

	var b strings.Builder
	b.WriteString(titleStyle.Render("s3lf — keybindings"))
	b.WriteString("\n\n")
	for i, s := range helpSections {
		b.WriteString(titleStyle.Render(s.title))
		b.WriteString("\n")
		for _, bnd := range s.bindings {
			pad := maxKey - lipgloss.Width(bnd.keys) + 2
			b.WriteString("  ")
			b.WriteString(keyStyle.Render(bnd.keys))
			b.WriteString(strings.Repeat(" ", pad))
			b.WriteString(bnd.desc)
			b.WriteString("\n")
		}
		if i < len(helpSections)-1 {
			b.WriteString("\n")
		}
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render("press ? or esc to dismiss"))
	return b.String()
}
