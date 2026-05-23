package tui

import (
	"fmt"
	"os"
	"path"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mbbennis/s3lf/internal/browser"
	"github.com/mbbennis/s3lf/internal/cache"
	"github.com/mbbennis/s3lf/internal/ops"
	"github.com/mbbennis/s3lf/internal/s3fs"
)

// currentFile returns (bucket, key, basename) for the cursor entry on the
// current pane, if it's a file. Returns ok=false on directories, buckets,
// or while the listing is still loading.
func (a *App) currentFile() (bucket, key, name string, ok bool) {
	cur := a.model.Current()
	if cur.Path.IsRoot() {
		return "", "", "", false
	}
	entry, found := browser.VisibleEntry(a.cache, cur)
	if !found || entry.IsDir {
		return "", "", "", false
	}
	bucket = cur.Path.Bucket
	key = cur.Path.Prefix + entry.Name
	return bucket, key, entry.Name, true
}

// triggerDownload validates and fires a download for the current file.
func (a *App) triggerDownload() tea.Cmd {
	bucket, key, name, ok := a.currentFile()
	if !ok {
		a.status = "y: cursor not on a file"
		return nil
	}
	a.status = "downloading " + name + "…"
	return ops.Download(a.fs, bucket, key, a.downloadDir)
}

// triggerOpen hands the current file to the platform's default opener.
// Always allowed (non-destructive).
func (a *App) triggerOpen() tea.Cmd {
	bucket, key, name, ok := a.currentFile()
	if !ok {
		a.status = "o: cursor not on a file"
		return nil
	}
	a.status = "opening " + name + " with system default…"
	return ops.Open(a.fs, bucket, key)
}

// triggerView fires the describe step for view ($PAGER). Always allowed,
// including in read-only mode — viewing is non-destructive.
func (a *App) triggerView() tea.Cmd {
	bucket, key, name, ok := a.currentFile()
	if !ok {
		a.status = "v: cursor not on a file"
		return nil
	}
	a.status = "opening " + name + "…"
	return ops.DescribeForView(a.fs, bucket, key, a.editSizeLimit)
}

// triggerEdit validates and fires the describe step. Read-only mode refuses.
func (a *App) triggerEdit() tea.Cmd {
	if a.readOnly {
		a.status = "read-only: edit disabled"
		return nil
	}
	bucket, key, name, ok := a.currentFile()
	if !ok {
		a.status = "e: cursor not on a file"
		return nil
	}
	a.status = "opening " + name + "…"
	return ops.DescribeForEdit(a.fs, bucket, key, a.editSizeLimit)
}

// triggerDelete pushes the type-the-filename confirm prompt. Read-only refuses.
func (a *App) triggerDelete() tea.Cmd {
	if a.readOnly {
		a.status = "read-only: delete disabled"
		return nil
	}
	bucket, key, name, ok := a.currentFile()
	if !ok {
		a.status = "D: cursor not on a file"
		return nil
	}
	a.prompt = &prompt{
		label: fmt.Sprintf("delete %s? type the name to confirm: ", name),
		onSubmit: func(q string) tea.Cmd {
			if q != name {
				a.status = "delete aborted (name mismatch)"
				return nil
			}
			a.status = "deleting " + name + "…"
			return ops.Delete(a.fs, bucket, key)
		},
		onCancel: func() tea.Cmd {
			a.status = "delete cancelled"
			return nil
		},
	}
	return nil
}

// handleOpsMsg dispatches ops result messages. Returns nil if msg is not
// an ops message.
func (a *App) handleOpsMsg(msg tea.Msg) tea.Cmd {
	switch m := msg.(type) {
	case ops.DownloadDoneMsg:
		if m.Err != nil {
			a.status = "download: " + m.Err.Error()
			return nil
		}
		a.status = fmt.Sprintf("saved %s (%s)", m.Path, humanSize(m.Bytes))
		return nil

	case ops.DeleteDoneMsg:
		if m.Err != nil {
			a.status = "delete: " + m.Err.Error()
			return nil
		}
		a.status = "deleted " + path.Base(m.Key)
		// Refresh the listing so the deleted entry disappears.
		a.cache.Invalidate(cache.Key{Bucket: m.Bucket, Prefix: parentPrefix(m.Key)})
		return a.ensureLoaded()

	case ops.DescribedForEditMsg:
		return a.afterDescribeForEdit(m)

	case ops.DownloadedForEditMsg:
		return a.afterDownloadForEdit(m)

	case editorExitedMsg:
		return a.afterEditorExit(m)

	case ops.SavedAfterEditMsg:
		return a.afterSaveEdit(m)

	case ops.DescribedForViewMsg:
		return a.afterDescribeForView(m)

	case ops.DownloadedForViewMsg:
		return a.afterDownloadForView(m)

	case viewerExitedMsg:
		return a.afterViewerExit(m)

	case ops.OpenedMsg:
		if m.Err != nil {
			a.status = "open: " + m.Err.Error()
			return nil
		}
		a.status = "opened " + path.Base(m.Key)
		return nil
	}
	return nil
}

func (a *App) afterDescribeForEdit(m ops.DescribedForEditMsg) tea.Cmd {
	if m.Err != nil {
		a.status = "edit head: " + m.Err.Error()
		return nil
	}
	switch m.Class {
	case ops.EditClassTooLarge:
		a.status = fmt.Sprintf("too large for editor (%s); use 'y' to download", humanSize(m.Info.Size))
		return nil
	case ops.EditClassBinary:
		// Push a binary-confirm prompt. User types "yes" to proceed.
		ct := m.Info.ContentType
		display := ct
		if display == "" {
			display = "unknown"
		}
		bucket, key, etag := m.Bucket, m.Key, m.Info.ETag
		a.prompt = &prompt{
			label: fmt.Sprintf("open binary file (%s)? type 'yes' to confirm: ", display),
			onSubmit: func(q string) tea.Cmd {
				if q != "yes" {
					a.status = "edit cancelled"
					return nil
				}
				a.status = "downloading…"
				return ops.DownloadForEdit(a.fs, bucket, key, etag, ct)
			},
			onCancel: func() tea.Cmd { a.status = "edit cancelled"; return nil },
		}
		return nil
	case ops.EditClassText:
		a.status = "downloading…"
		return ops.DownloadForEdit(a.fs, m.Bucket, m.Key, m.Info.ETag, m.Info.ContentType)
	}
	return nil
}

func (a *App) afterDownloadForEdit(m ops.DownloadedForEditMsg) tea.Cmd {
	if m.Err != nil {
		if s3fs.IsPreconditionFailed(m.Err) {
			a.status = "object changed since open; press 'e' to retry"
			return nil
		}
		a.status = "edit download: " + m.Err.Error()
		return nil
	}
	cmd, err := ops.EditorCmd(m.Path)
	if err != nil {
		a.status = err.Error()
		cleanupTemp(m.Path)
		return nil
	}
	// Stash so the exec callback (and the save step) has the metadata.
	// ContentType is preserved from the original; if blank, leave blank so
	// S3 doesn't get a wrong value back.
	a.pendingEdit = &pendingEdit{
		bucket: m.Bucket, key: m.Key, path: m.Path,
		etag: m.ETag, hash: m.Hash, contentType: m.ContentType,
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return editorExitedMsg{err: err}
	})
}

type editorExitedMsg struct{ err error }

func (a *App) afterEditorExit(m editorExitedMsg) tea.Cmd {
	pe := a.pendingEdit
	if pe == nil {
		return nil
	}
	if m.err != nil {
		a.status = "editor: " + m.err.Error()
		a.pendingEdit = nil
		return nil
	}
	a.status = "saving…"
	saveCmd := ops.SaveAfterEdit(a.fs, pe.bucket, pe.key, pe.path, pe.hash, pe.etag, pe.contentType)
	a.pendingEdit = nil
	return saveCmd
}

func (a *App) afterSaveEdit(m ops.SavedAfterEditMsg) tea.Cmd {
	switch {
	case m.Err != nil && m.Conflict:
		a.status = "s3 changed since open; local edits at " + m.LocalPath
		return nil
	case m.Err != nil:
		a.status = "save: " + m.Err.Error()
		return nil
	case m.Unchanged:
		a.status = "no changes"
		return nil
	default:
		a.status = "saved " + path.Base(m.Key)
		// Invalidate so the new size/mtime show up.
		a.cache.Invalidate(cache.Key{Bucket: m.Bucket, Prefix: parentPrefix(m.Key)})
		return a.ensureLoaded()
	}
}

// cleanupTemp removes the temp file's enclosing dir (ops.edit's layout:
// one file per dir).
func cleanupTemp(p string) {
	_ = os.RemoveAll(filepath.Dir(p))
}

func (a *App) afterDescribeForView(m ops.DescribedForViewMsg) tea.Cmd {
	if m.Err != nil {
		a.status = "view head: " + m.Err.Error()
		return nil
	}
	switch m.Class {
	case ops.EditClassTooLarge:
		a.status = fmt.Sprintf("too large for viewer (%s); use 'y' to download", humanSize(m.Info.Size))
		return nil
	case ops.EditClassBinary:
		ct := m.Info.ContentType
		if ct == "" {
			ct = "unknown"
		}
		bucket, key := m.Bucket, m.Key
		a.prompt = &prompt{
			label: fmt.Sprintf("view binary file (%s)? type 'yes' to confirm: ", ct),
			onSubmit: func(q string) tea.Cmd {
				if q != "yes" {
					a.status = "view cancelled"
					return nil
				}
				a.status = "downloading…"
				return ops.DownloadForView(a.fs, bucket, key)
			},
			onCancel: func() tea.Cmd { a.status = "view cancelled"; return nil },
		}
		return nil
	case ops.EditClassText:
		a.status = "downloading…"
		return ops.DownloadForView(a.fs, m.Bucket, m.Key)
	}
	return nil
}

func (a *App) afterDownloadForView(m ops.DownloadedForViewMsg) tea.Cmd {
	if m.Err != nil {
		a.status = "view download: " + m.Err.Error()
		return nil
	}
	cmd, err := ops.PagerCmd(m.Path)
	if err != nil {
		a.status = err.Error()
		cleanupTemp(m.Path)
		return nil
	}
	path := m.Path
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		return viewerExitedMsg{path: path, err: err}
	})
}

type viewerExitedMsg struct {
	path string
	err  error
}

func (a *App) afterViewerExit(m viewerExitedMsg) tea.Cmd {
	cleanupTemp(m.path)
	if m.err != nil {
		a.status = "viewer: " + m.err.Error()
		return nil
	}
	a.status = ""
	return nil
}

// parentPrefix returns the directory portion of an S3 key — everything up
// to and including the last "/". For root-level objects, returns "".
func parentPrefix(key string) string {
	for i := len(key) - 1; i >= 0; i-- {
		if key[i] == '/' {
			return key[:i+1]
		}
	}
	return ""
}

