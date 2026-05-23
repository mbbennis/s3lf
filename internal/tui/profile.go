package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/mbbennis/s3lf/internal/awscfg"
	"github.com/mbbennis/s3lf/internal/browser"
	"github.com/mbbennis/s3lf/internal/cache"
)

// openProfilePicker pulls the list of available profiles from ~/.aws/* and
// shows the picker. Disabled (status only) if no switch callback was wired.
func (a *App) openProfilePicker() {
	if a.switchProfile == nil {
		a.status = "profile switching not available"
		return
	}
	profiles, err := awscfg.List()
	if err != nil {
		a.status = "list profiles: " + err.Error()
		return
	}
	if len(profiles) == 0 {
		a.status = "no profiles found in ~/.aws/config or ~/.aws/credentials"
		return
	}

	// Position the cursor on the active profile if it's in the list.
	cursor := 0
	for i, p := range profiles {
		if p == a.profile {
			cursor = i
			break
		}
	}

	a.picker = &picker{
		label:  "switch profile",
		items:  profiles,
		cursor: cursor,
		onSelect: func(name string) tea.Cmd {
			if name == a.profile {
				a.status = "already on profile " + name
				return nil
			}
			c, fs, err := a.switchProfile(name)
			if err != nil {
				a.status = "switch " + name + ": " + err.Error()
				return nil
			}
			// Replace the per-profile state atomically. Any in-flight fetches
			// against the old client write to the old cache and are dropped
			// on the floor — no-op for the UI.
			a.cache = c
			a.fs = fs
			a.profile = name
			a.model = browser.New()
			a.inflight = map[cache.Key]bool{}
			a.inflightNext = map[cache.Key]bool{}
			a.status = "switched to profile " + name
			return a.ensureLoaded()
		},
	}
}
