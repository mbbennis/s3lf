package ops

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mbbennis/s3lf/internal/s3fs"
)

// Download returns a Cmd that downloads (bucket, key) into destDir. If destDir
// is empty, the current working directory is used. The destination filename
// is the key's basename; if the file exists the name gets " (N)" appended.
func Download(fs s3fs.FS, bucket, key, destDir string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()

		dir := destDir
		if dir == "" {
			cwd, err := os.Getwd()
			if err != nil {
				return DownloadDoneMsg{Bucket: bucket, Key: key, Err: err}
			}
			dir = cwd
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return DownloadDoneMsg{Bucket: bucket, Key: key, Err: err}
		}

		dest := uniquePath(filepath.Join(dir, path.Base(key)))
		f, err := os.Create(dest)
		if err != nil {
			return DownloadDoneMsg{Bucket: bucket, Key: key, Err: err}
		}
		n, err := fs.Download(ctx, bucket, key, f, "")
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			_ = os.Remove(dest)
			return DownloadDoneMsg{Bucket: bucket, Key: key, Err: err}
		}
		return DownloadDoneMsg{Bucket: bucket, Key: key, Path: dest, Bytes: n}
	}
}

// uniquePath appends " (1)", " (2)" before the extension until the path
// doesn't exist.
func uniquePath(p string) string {
	if _, err := os.Stat(p); os.IsNotExist(err) {
		return p
	}
	dir, base := filepath.Split(p)
	ext := filepath.Ext(base)
	stem := base[:len(base)-len(ext)]
	for i := 1; i < 10000; i++ {
		candidate := filepath.Join(dir, fmt.Sprintf("%s (%d)%s", stem, i, ext))
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return p // give up; will overwrite
}
