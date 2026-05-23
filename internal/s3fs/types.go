package s3fs

import "time"

// Entry is a single row in a listing — either a "directory" (CommonPrefix)
// or an object. Directories have IsDir=true and zero Size/Modified.
type Entry struct {
	Name     string
	IsDir    bool
	Size     int64
	Modified time.Time
	Storage  string
}

// Listing is the result of one ListObjectsV2 call (one page). The cache
// stitches additional pages onto the same listing when filter mode or the
// user demands them.
type Listing struct {
	Bucket    string
	Prefix    string
	Entries   []Entry
	NextToken string // empty when fully enumerated
	Complete  bool   // true once NextToken is empty and all pages joined
	FetchedAt time.Time
}

// Bucket is the result of a ListBuckets call.
type Bucket struct {
	Name    string
	Region  string // may be empty until resolved
	Created time.Time
}

// ObjectInfo is the minimum we need to make edit/download decisions before
// streaming the body: how big, what kind, and the ETag for conditional writes.
type ObjectInfo struct {
	Size        int64
	ETag        string
	ContentType string
	Modified    time.Time
}
