package ops

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mbbennis/s3lf/internal/s3fs"
)

// Delete returns a Cmd that deletes (bucket, key). The caller is responsible
// for confirmation; this just fires the API call.
func Delete(fs s3fs.FS, bucket, key string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		err := fs.Delete(ctx, bucket, key)
		return DeleteDoneMsg{Bucket: bucket, Key: key, Err: err}
	}
}
