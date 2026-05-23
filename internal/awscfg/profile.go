// Package awscfg is the lightweight wrapper between the AWS SDK and the
// rest of the app: profile discovery and on-demand config loading. Keeps
// SDK imports out of the TUI/orchestration layer except where they need
// to construct concrete s3fs.Client instances.
package awscfg

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
)

// List returns the profile names defined in ~/.aws/config and
// ~/.aws/credentials, deduped and sorted with "default" first. Missing
// files are ignored — a user with credentials in only one file is normal.
func List() ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	seen := map[string]struct{}{}
	if err := readProfiles(filepath.Join(home, ".aws", "config"), true, seen); err != nil {
		return nil, err
	}
	if err := readProfiles(filepath.Join(home, ".aws", "credentials"), false, seen); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i] == "default" {
			return true
		}
		if out[j] == "default" {
			return false
		}
		return out[i] < out[j]
	})
	return out, nil
}

// Effective returns the profile name the SDK would pick if no override is
// given: $AWS_PROFILE, then $AWS_DEFAULT_PROFILE, then "default".
func Effective() string {
	if v := os.Getenv("AWS_PROFILE"); v != "" {
		return v
	}
	if v := os.Getenv("AWS_DEFAULT_PROFILE"); v != "" {
		return v
	}
	return "default"
}

// Load builds an aws.Config for the given profile (or the SDK default when
// profile == ""). region overrides whatever the profile resolves to, if set.
func Load(ctx context.Context, profile, region string) (aws.Config, error) {
	var opts []func(*awsconfig.LoadOptions) error
	if profile != "" {
		opts = append(opts, awsconfig.WithSharedConfigProfile(profile))
	}
	if region != "" {
		opts = append(opts, awsconfig.WithRegion(region))
	}
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}

// readProfiles scans an INI-style file for [section] headers and records
// the profile name in seen. Both files use "[default]" for the default;
// ~/.aws/config additionally uses "[profile NAME]" for the rest, while
// ~/.aws/credentials uses "[NAME]" directly. needsPrefix selects which
// convention to apply.
func readProfiles(path string, needsPrefix bool, seen map[string]struct{}) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read %s: %w", path, err)
	}
	defer f.Close()

	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(line, "[") || !strings.HasSuffix(line, "]") {
			continue
		}
		name := strings.TrimSpace(line[1 : len(line)-1])
		if name == "default" {
			seen[name] = struct{}{}
			continue
		}
		if needsPrefix {
			// ~/.aws/config: only "[profile NAME]" entries are named profiles.
			// Other sections like [sso-session NAME] or [services NAME] are
			// skipped here.
			const p = "profile "
			if !strings.HasPrefix(name, p) {
				continue
			}
			name = strings.TrimSpace(name[len(p):])
		}
		if name != "" {
			seen[name] = struct{}{}
		}
	}
	return s.Err()
}
