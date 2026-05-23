package s3fs

import (
	"context"
	"io"
)

// FS is the abstraction the rest of the app codes against. Anything that
// imports this package must NOT need the AWS SDK — that's confined to
// client.go behind this interface.
type FS interface {
	ListBuckets(ctx context.Context) ([]Bucket, error)

	// List returns one page of entries at (bucket, prefix). Pass an empty
	// token for the first page; the returned Listing's NextToken feeds the
	// next call.
	List(ctx context.Context, bucket, prefix, token string) (*Listing, error)

	// Region resolves the region of a bucket. Implementations should cache.
	Region(ctx context.Context, bucket string) (string, error)

	// HeadObject returns size/etag/content-type without transferring the body.
	HeadObject(ctx context.Context, bucket, key string) (*ObjectInfo, error)

	// Download streams the object to w. For large objects implementations
	// should use multipart parallel download. Returns the number of bytes
	// written. If ifMatch is non-empty, the download is conditional on the
	// object's current ETag — a mismatch returns a PreconditionFailed error
	// detectable via IsPreconditionFailed, so callers can re-HEAD and retry.
	Download(ctx context.Context, bucket, key string, w io.WriterAt, ifMatch string) (int64, error)

	// Upload puts r at (bucket, key). If ifMatch is non-empty the upload is
	// conditional on the existing object's ETag — a mismatch returns a
	// PreconditionFailed error that callers MUST check for via IsPreconditionFailed.
	Upload(ctx context.Context, bucket, key string, r io.Reader, size int64, contentType, ifMatch string) error

	// Delete removes a single object.
	Delete(ctx context.Context, bucket, key string) error
}

// IsPreconditionFailed reports whether err is a 412 from a conditional
// PutObject (edit-save conflict). Implementations should set this so the
// TUI can present a useful message without depending on the SDK.
func IsPreconditionFailed(err error) bool {
	if err == nil {
		return false
	}
	var pf interface{ PreconditionFailed() bool }
	if errAs(err, &pf) && pf.PreconditionFailed() {
		return true
	}
	return false
}
