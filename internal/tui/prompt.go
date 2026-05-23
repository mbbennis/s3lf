package tui

import tea "github.com/charmbracelet/bubbletea"

// prompt is a one-line input handler. While App.prompt is non-nil, all
// keystrokes are routed here instead of the normal keymap.
//
// The three callbacks let each call site (search, delete-confirm, binary-
// confirm) drive its own logic without growing a mode enum.
type prompt struct {
	label    string
	query    string
	onChange func(query string) tea.Cmd
	onSubmit func(query string) tea.Cmd
	onCancel func() tea.Cmd
}

func (p *prompt) render() string {
	return p.label + p.query
}
