package ops

import "testing"

func TestIsTextContentType(t *testing.T) {
	textyTrue := []string{
		"text/plain",
		"text/csv",
		"text/markdown; charset=utf-8",
		"application/json",
		"application/xml",
		"application/yaml",
		"application/x-yaml",
		"application/toml",
		"application/javascript",
		"application/x-sh",
		"application/sql",
		"TEXT/PLAIN", // case-insensitive
	}
	for _, ct := range textyTrue {
		if !isTextContentType(ct) {
			t.Errorf("%q: expected text-y, got binary", ct)
		}
	}

	textyFalse := []string{
		"",                        // unknown — should prompt
		"binary/octet-stream",     // default for raw uploads — should prompt
		"application/octet-stream",
		"image/png",
		"application/zip",
		"video/mp4",
		"application/pdf",
	}
	for _, ct := range textyFalse {
		if isTextContentType(ct) {
			t.Errorf("%q: expected binary, got text-y", ct)
		}
	}
}
