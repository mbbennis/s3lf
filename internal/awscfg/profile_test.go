package awscfg

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeFile writes content to dir/path, creating intermediate dirs.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	full := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// withHome rewrites HOME for the duration of the test and returns the new
// home directory. Lets List() find synthetic config files.
func withHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	return dir
}

func TestList_BothFiles(t *testing.T) {
	home := withHome(t)
	writeFile(t, home, ".aws/config", `
[default]
region = us-east-1

[profile staging]
region = us-west-2

[profile prod]
region = eu-west-1

[sso-session main]
sso_start_url = https://example.com
`)
	writeFile(t, home, ".aws/credentials", `
[default]
aws_access_key_id = AKIA...
aws_secret_access_key = ...

[work]
aws_access_key_id = AKIA...
aws_secret_access_key = ...
`)

	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"default", "prod", "staging", "work"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %v, want %v", got, want)
	}
}

func TestList_DefaultFirst(t *testing.T) {
	home := withHome(t)
	// Order in the files shouldn't matter — default always comes first.
	writeFile(t, home, ".aws/config", `
[profile zeta]
region = us-east-1

[default]
region = us-east-1
`)
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 || got[0] != "default" {
		t.Errorf("expected 'default' first, got %v", got)
	}
}

func TestList_MissingFilesOK(t *testing.T) {
	withHome(t) // tempdir with no .aws/
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty list, got %v", got)
	}
}

func TestList_SkipsNonProfileSections(t *testing.T) {
	home := withHome(t)
	writeFile(t, home, ".aws/config", `
[sso-session main]
sso_start_url = https://example.com

[services my-service]
s3 =
  endpoint_url = http://localhost:4566

[profile keep]
region = us-east-1
`)
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"keep"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %v, want %v", got, want)
	}
}

func TestList_DedupesAcrossFiles(t *testing.T) {
	home := withHome(t)
	writeFile(t, home, ".aws/config", `[profile shared]
region = us-east-1
`)
	writeFile(t, home, ".aws/credentials", `[shared]
aws_access_key_id = AKIA...
aws_secret_access_key = ...
`)
	got, err := List()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"shared"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("List() = %v, want %v", got, want)
	}
}

func TestEffective(t *testing.T) {
	// Unset both to confirm fallback.
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_DEFAULT_PROFILE", "")
	if got := Effective(); got != "default" {
		t.Errorf("Effective() with both unset = %q, want default", got)
	}

	t.Setenv("AWS_DEFAULT_PROFILE", "fallback")
	if got := Effective(); got != "fallback" {
		t.Errorf("Effective() with AWS_DEFAULT_PROFILE = %q, want fallback", got)
	}

	t.Setenv("AWS_PROFILE", "primary")
	if got := Effective(); got != "primary" {
		t.Errorf("Effective() with AWS_PROFILE set = %q, want primary", got)
	}
}
