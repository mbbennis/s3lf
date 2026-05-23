package s3fs

import "testing"

func TestPath_IsRootAndString(t *testing.T) {
	cases := []struct {
		p    Path
		root bool
		str  string
	}{
		{Path{}, true, "s3://"},
		{Path{Bucket: "foo"}, false, "s3://foo/"},
		{Path{Bucket: "foo", Prefix: "a/"}, false, "s3://foo/a/"},
		{Path{Bucket: "foo", Prefix: "a/b/"}, false, "s3://foo/a/b/"},
	}
	for _, c := range cases {
		if got := c.p.IsRoot(); got != c.root {
			t.Errorf("%+v.IsRoot() = %v, want %v", c.p, got, c.root)
		}
		if got := c.p.String(); got != c.str {
			t.Errorf("%+v.String() = %q, want %q", c.p, got, c.str)
		}
	}
}

func TestPath_Parent(t *testing.T) {
	cases := []struct {
		in, want Path
	}{
		{Path{}, Path{}},                                                     // root → root
		{Path{Bucket: "foo"}, Path{}},                                        // bucket → root
		{Path{Bucket: "foo", Prefix: "a/"}, Path{Bucket: "foo"}},             // top-level prefix → bucket
		{Path{Bucket: "foo", Prefix: "a/b/"}, Path{Bucket: "foo", Prefix: "a/"}}, // nested → parent
		{Path{Bucket: "foo", Prefix: "a/b/c/"}, Path{Bucket: "foo", Prefix: "a/b/"}},
	}
	for _, c := range cases {
		if got := c.in.Parent(); got != c.want {
			t.Errorf("%+v.Parent() = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestPath_Child(t *testing.T) {
	cases := []struct {
		in   Path
		name string
		want Path
	}{
		{Path{}, "mybucket", Path{Bucket: "mybucket"}},
		{Path{Bucket: "foo"}, "a", Path{Bucket: "foo", Prefix: "a/"}},
		{Path{Bucket: "foo", Prefix: "a/"}, "b", Path{Bucket: "foo", Prefix: "a/b/"}},
		{Path{Bucket: "foo", Prefix: "a/b/"}, "c", Path{Bucket: "foo", Prefix: "a/b/c/"}},
	}
	for _, c := range cases {
		if got := c.in.Child(c.name); got != c.want {
			t.Errorf("%+v.Child(%q) = %+v, want %+v", c.in, c.name, got, c.want)
		}
	}
}

func TestPath_Leaf(t *testing.T) {
	cases := []struct {
		in   Path
		want string
	}{
		{Path{}, "/"},
		{Path{Bucket: "foo"}, "foo"},
		{Path{Bucket: "foo", Prefix: "a/"}, "a"},
		{Path{Bucket: "foo", Prefix: "a/b/"}, "b"},
		{Path{Bucket: "foo", Prefix: "deep/nested/folder/"}, "folder"},
	}
	for _, c := range cases {
		if got := c.in.Leaf(); got != c.want {
			t.Errorf("%+v.Leaf() = %q, want %q", c.in, got, c.want)
		}
	}
}

// Parent-of-Child should round-trip when starting from a non-root path.
func TestPath_ParentOfChildRoundTrip(t *testing.T) {
	starts := []Path{
		{Bucket: "foo"},
		{Bucket: "foo", Prefix: "a/"},
		{Bucket: "foo", Prefix: "a/b/"},
	}
	for _, s := range starts {
		got := s.Child("x").Parent()
		if got != s {
			t.Errorf("%+v.Child(x).Parent() = %+v, want %+v", s, got, s)
		}
	}
}
