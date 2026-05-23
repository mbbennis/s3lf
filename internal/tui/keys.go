package tui

import tea "github.com/charmbracelet/bubbletea"

// matchKey is a tiny helper for "did the user press this key (or one of
// these keys)?". Keeps Update readable.
func matchKey(m tea.KeyMsg, keys ...string) bool {
	s := m.String()
	for _, k := range keys {
		if s == k {
			return true
		}
	}
	return false
}
