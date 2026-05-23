package s3fs

import "errors"

// errAs is a tiny wrapper so fs.go can call errors.As without importing
// errors at the top — keeps fs.go free of unrelated machinery.
func errAs(err error, target any) bool { return errors.As(err, target) }

// preconditionErr is the error type our Client wraps SDK 412 responses in,
// so callers detect conflicts via IsPreconditionFailed without depending
// on the SDK's error types.
type preconditionErr struct{ wrapped error }

func (e *preconditionErr) Error() string                  { return e.wrapped.Error() }
func (e *preconditionErr) Unwrap() error                  { return e.wrapped }
func (e *preconditionErr) PreconditionFailed() bool       { return true }
