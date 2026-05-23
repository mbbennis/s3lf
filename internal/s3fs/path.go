package s3fs

import "strings"

// Path identifies a location in the S3 namespace. Bucket == "" means the
// root (the bucket list). Prefix is always either empty or ends in "/".
type Path struct {
	Bucket string
	Prefix string
}

func (p Path) IsRoot() bool { return p.Bucket == "" }

func (p Path) String() string {
	if p.Bucket == "" {
		return "s3://"
	}
	return "s3://" + p.Bucket + "/" + p.Prefix
}

// Parent returns the path one level up. Parent of root is root.
func (p Path) Parent() Path {
	if p.Bucket == "" {
		return p
	}
	if p.Prefix == "" {
		return Path{}
	}
	trimmed := strings.TrimSuffix(p.Prefix, "/")
	i := strings.LastIndex(trimmed, "/")
	if i < 0 {
		return Path{Bucket: p.Bucket, Prefix: ""}
	}
	return Path{Bucket: p.Bucket, Prefix: trimmed[:i+1]}
}

// Child returns the path obtained by descending into a directory entry.
// name must not contain a trailing slash.
func (p Path) Child(name string) Path {
	if p.Bucket == "" {
		return Path{Bucket: name}
	}
	return Path{Bucket: p.Bucket, Prefix: p.Prefix + name + "/"}
}

// Leaf returns the last segment of the prefix — useful as a pane title.
func (p Path) Leaf() string {
	if p.Bucket == "" {
		return "/"
	}
	if p.Prefix == "" {
		return p.Bucket
	}
	trimmed := strings.TrimSuffix(p.Prefix, "/")
	if i := strings.LastIndex(trimmed, "/"); i >= 0 {
		return trimmed[i+1:]
	}
	return trimmed
}
