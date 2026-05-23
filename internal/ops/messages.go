package ops

import "github.com/mbbennis/s3lf/internal/s3fs"

// Messages emitted by ops.* Cmd functions. The TUI's Update is the sole
// consumer — keeping these public lets ops stay free of bubbletea imports.

type DownloadDoneMsg struct {
	Bucket string
	Key    string
	Path   string // local file path on success
	Bytes  int64
	Err    error
}

type DeleteDoneMsg struct {
	Bucket string
	Key    string
	Err    error
}

// EditClass tags the editor-suitability of an object based on its
// Content-Type. We don't sniff bytes — the user opted for content-type only.
type EditClass int

const (
	EditClassText EditClass = iota
	EditClassBinary
	EditClassTooLarge
)

type DescribedForEditMsg struct {
	Bucket string
	Key    string
	Info   *s3fs.ObjectInfo
	Class  EditClass
	Err    error
}

type DownloadedForEditMsg struct {
	Bucket      string
	Key         string
	Path        string // temp file
	ETag        string
	Hash        string // sha256 of original bytes, used to detect modification
	ContentType string // captured from the HEAD so we can preserve it on save
	Err         error
}

type SavedAfterEditMsg struct {
	Bucket    string
	Key       string
	Conflict  bool   // true iff S3 ETag changed (If-Match failed)
	Unchanged bool   // true iff editor exited without modifying the file
	LocalPath string // preserved on conflict; empty on success
	Err       error
}

// View flow parallels edit but skips the modification/save step.

type DescribedForViewMsg struct {
	Bucket string
	Key    string
	Info   *s3fs.ObjectInfo
	Class  EditClass
	Err    error
}

type DownloadedForViewMsg struct {
	Bucket string
	Key    string
	Path   string
	Err    error
}
