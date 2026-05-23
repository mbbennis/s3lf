package ops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mbbennis/s3lf/internal/s3fs"
)

// DescribeForEdit returns a Cmd that HEADs the object and classifies it.
// The TUI uses the result to decide whether to refuse (too large), prompt
// (binary), or proceed directly to DownloadForEdit.
func DescribeForEdit(fs s3fs.FS, bucket, key string, sizeLimit int64) tea.Cmd {
	return describe(fs, bucket, key, sizeLimit, false)
}

// DescribeForView is the view-flow counterpart. Same checks, distinct
// result type so the TUI dispatches differently.
func DescribeForView(fs s3fs.FS, bucket, key string, sizeLimit int64) tea.Cmd {
	return describe(fs, bucket, key, sizeLimit, true)
}

func describe(fs s3fs.FS, bucket, key string, sizeLimit int64, view bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		info, err := fs.HeadObject(ctx, bucket, key)
		class := EditClassText
		if err == nil {
			switch {
			case info.Size > sizeLimit:
				class = EditClassTooLarge
			case isTextContentType(info.ContentType):
				class = EditClassText
			default:
				class = EditClassBinary
			}
		}
		if view {
			return DescribedForViewMsg{Bucket: bucket, Key: key, Info: info, Class: class, Err: err}
		}
		return DescribedForEditMsg{Bucket: bucket, Key: key, Info: info, Class: class, Err: err}
	}
}

// DownloadForEdit downloads (bucket, key) to a unique temp file using a
// conditional GET keyed on etag (from the prior Describe step). On 412 the
// object has changed since Describe — surface the precondition error so
// the TUI can ask the user to retry, rather than handing the editor stale
// bytes paired with an out-of-date ETag. ContentType comes from Describe
// and is round-tripped so save can preserve it.
func DownloadForEdit(fs s3fs.FS, bucket, key, etag, contentType string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		dir, err := os.MkdirTemp("", "s3lf-edit-")
		if err != nil {
			return DownloadedForEditMsg{Bucket: bucket, Key: key, Err: err}
		}
		dest := filepath.Join(dir, path.Base(key))
		f, err := os.Create(dest)
		if err != nil {
			_ = os.RemoveAll(dir)
			return DownloadedForEditMsg{Bucket: bucket, Key: key, Err: err}
		}
		_, derr := fs.Download(ctx, bucket, key, f, etag)
		if cerr := f.Close(); derr == nil {
			derr = cerr
		}
		if derr != nil {
			_ = os.RemoveAll(dir)
			return DownloadedForEditMsg{Bucket: bucket, Key: key, Err: derr}
		}

		hash, err := fileSHA256(dest)
		if err != nil {
			_ = os.RemoveAll(dir)
			return DownloadedForEditMsg{Bucket: bucket, Key: key, Err: err}
		}
		return DownloadedForEditMsg{
			Bucket: bucket, Key: key, Path: dest,
			ETag: etag, Hash: hash, ContentType: contentType,
		}
	}
}

// DownloadForView mirrors DownloadForEdit but skips the second HEAD (no
// ETag/hash needed — we won't save back) and returns a view-specific msg.
func DownloadForView(fs s3fs.FS, bucket, key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		dir, err := os.MkdirTemp("", "s3lf-view-")
		if err != nil {
			return DownloadedForViewMsg{Bucket: bucket, Key: key, Err: err}
		}
		dest := filepath.Join(dir, path.Base(key))
		f, err := os.Create(dest)
		if err != nil {
			return DownloadedForViewMsg{Bucket: bucket, Key: key, Err: err}
		}
		_, err = fs.Download(ctx, bucket, key, f, "")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.RemoveAll(dir)
			return DownloadedForViewMsg{Bucket: bucket, Key: key, Err: err}
		}
		return DownloadedForViewMsg{Bucket: bucket, Key: key, Path: dest}
	}
}

// EditorCmd builds an *exec.Cmd for $EDITOR with the given file. Returns
// an error if $EDITOR is unset — we don't pick a default because the
// fallback that's right for one user (vi) is wrong for many others.
func EditorCmd(path string) (*exec.Cmd, error) {
	return envCmd("EDITOR", path)
}

// PagerCmd builds an *exec.Cmd for $PAGER. Same no-default policy as
// EditorCmd — caller surfaces the unset error to the user.
func PagerCmd(path string) (*exec.Cmd, error) {
	return envCmd("PAGER", path)
}

// envCmd reads an env var that may contain a command + flags ("code --wait",
// "less -R") and returns a runnable exec.Cmd appending path as the last arg.
func envCmd(envVar, path string) (*exec.Cmd, error) {
	v := strings.TrimSpace(os.Getenv(envVar))
	if v == "" {
		return nil, &UnsetEnvError{Var: envVar}
	}
	parts := strings.Fields(v)
	args := append(parts[1:], path)
	return exec.Command(parts[0], args...), nil
}

// UnsetEnvError is returned when a required env var is unset. The TUI
// surfaces .Var so the user sees which one to set.
type UnsetEnvError struct{ Var string }

func (e *UnsetEnvError) Error() string { return "$" + e.Var + " is not set" }

// SaveAfterEdit checks the temp file for modification (sha256 vs original)
// and, if changed, uploads it back with If-Match against the original ETag.
// Always cleans up the temp dir on success; preserves it on conflict so the
// user can recover.
func SaveAfterEdit(fs s3fs.FS, bucket, key, path, originalHash, etag, contentType string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		curHash, err := fileSHA256(path)
		if err != nil {
			return SavedAfterEditMsg{Bucket: bucket, Key: key, LocalPath: path, Err: err}
		}
		if curHash == originalHash {
			cleanupTempDir(path)
			return SavedAfterEditMsg{Bucket: bucket, Key: key, Unchanged: true}
		}

		// Stream the file back. Capture size for the upload metadata.
		data, err := os.ReadFile(path)
		if err != nil {
			return SavedAfterEditMsg{Bucket: bucket, Key: key, LocalPath: path, Err: err}
		}
		err = fs.Upload(ctx, bucket, key, bytes.NewReader(data), int64(len(data)), contentType, etag)
		if err != nil {
			if s3fs.IsPreconditionFailed(err) {
				return SavedAfterEditMsg{
					Bucket: bucket, Key: key, Conflict: true, LocalPath: path, Err: err,
				}
			}
			return SavedAfterEditMsg{Bucket: bucket, Key: key, LocalPath: path, Err: err}
		}
		cleanupTempDir(path)
		return SavedAfterEditMsg{Bucket: bucket, Key: key}
	}
}

func cleanupTempDir(p string) {
	// The temp file lives one level inside the temp dir we created; remove
	// the whole dir to also drop the file.
	_ = os.RemoveAll(filepath.Dir(p))
}

func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isTextContentType is intentionally narrow: only proceed without a binary
// warning when we're confident the object is text. Unknown / empty /
// octet-stream all fall through to "binary" so the user gets a prompt.
func isTextContentType(ct string) bool {
	if ct == "" {
		return false
	}
	// Strip parameters like "; charset=utf-8".
	mediatype, _, err := mime.ParseMediaType(ct)
	if err != nil {
		mediatype = strings.ToLower(strings.TrimSpace(strings.SplitN(ct, ";", 2)[0]))
	}
	if strings.HasPrefix(mediatype, "text/") {
		return true
	}
	switch mediatype {
	case
		"application/json",
		"application/xml",
		"application/yaml",
		"application/x-yaml",
		"application/toml",
		"application/x-toml",
		"application/javascript",
		"application/x-javascript",
		"application/x-sh",
		"application/x-shellscript",
		"application/x-www-form-urlencoded",
		"application/sql":
		return true
	}
	return false
}

