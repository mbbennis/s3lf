package ops

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mbbennis/s3lf/internal/s3fs"
)

// OpenedMsg is the result of an Open op. The TUI uses it to update the
// statusbar. Path is the temp file we wrote (kept on disk so the GUI app
// can read it after the opener exits — OS temp cleanup eventually wins).
type OpenedMsg struct {
	Bucket string
	Key    string
	Path   string
	Err    error
}

// Open downloads (bucket, key) to a temp file and shells out to the
// platform's default opener (open / xdg-open / start) without suspending
// the TUI — those tools return immediately after handing the file to the
// GUI app, so tea.ExecProcess would just cause a screen flash for nothing.
//
// No size or content-type checks: the user pressed o to bypass the editor;
// they know what they're doing. Read-only mode is allowed.
func Open(fs s3fs.FS, bucket, key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		dir, err := os.MkdirTemp("", "s3lf-open-")
		if err != nil {
			return OpenedMsg{Bucket: bucket, Key: key, Err: err}
		}
		dest := filepath.Join(dir, path.Base(key))
		f, err := os.Create(dest)
		if err != nil {
			_ = os.RemoveAll(dir)
			return OpenedMsg{Bucket: bucket, Key: key, Err: err}
		}
		_, derr := fs.Download(ctx, bucket, key, f, "")
		if cerr := f.Close(); derr == nil {
			derr = cerr
		}
		if derr != nil {
			_ = os.RemoveAll(dir)
			return OpenedMsg{Bucket: bucket, Key: key, Err: derr}
		}

		cmd, err := openerCmd(dest)
		if err != nil {
			_ = os.RemoveAll(dir)
			return OpenedMsg{Bucket: bucket, Key: key, Err: err}
		}
		// Start, don't wait: the opener typically forks the GUI app and exits
		// immediately. Waiting would block this Cmd's goroutine; not waiting
		// is fine because we don't care about the opener's exit code.
		if err := cmd.Start(); err != nil {
			_ = os.RemoveAll(dir)
			return OpenedMsg{Bucket: bucket, Key: key, Err: err}
		}
		// Release the child so it doesn't become a zombie if it actually
		// outlives us (rare — most openers exec into another process).
		go func() { _ = cmd.Wait() }()
		return OpenedMsg{Bucket: bucket, Key: key, Path: dest}
	}
}

// openerCmd returns the platform's default-app launcher for path. The GUI
// apps these launchers hand off to may keep the file open after the
// launcher exits, so we leave temp files in os.TempDir for the OS to GC.
func openerCmd(path string) (*exec.Cmd, error) {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", path), nil
	case "linux":
		return exec.Command("xdg-open", path), nil
	case "windows":
		// `start ""` ensures the first arg isn't treated as a window title.
		return exec.Command("cmd", "/c", "start", "", path), nil
	default:
		return nil, fmt.Errorf("no system opener known for %s", runtime.GOOS)
	}
}
