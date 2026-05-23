package tui

import "testing"

func TestSmartcaseContains(t *testing.T) {
	cases := []struct {
		haystack, needle string
		want             bool
	}{
		// Lowercase needle → case-insensitive (matches either case).
		{"Foo Bar", "foo", true},
		{"FOO", "foo", true},
		// Uppercase in needle → case-sensitive.
		{"foo", "FOO", false},
		{"FOO", "FOO", true},
		{"Foo Bar", "Foo", true},
		{"foo bar", "Foo", false},
		{"FOOBAR", "OO", true},
		{"foobar", "OO", false},
		// Empty needle never matches (search jump is a no-op).
		{"anything", "", false},
		// Multi-byte (just confirm we don't panic; smartcase uses Contains on
		// the lowercased string, so ASCII-only is fine).
		{"héllo world", "héllo", true},
	}
	for _, c := range cases {
		if got := smartcaseContains(c.haystack, c.needle); got != c.want {
			t.Errorf("smartcase(%q,%q) = %v, want %v", c.haystack, c.needle, got, c.want)
		}
	}
}
