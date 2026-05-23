package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mbbennis/s3lf/internal/awscfg"
	"github.com/mbbennis/s3lf/internal/cache"
	"github.com/mbbennis/s3lf/internal/s3fs"
	"github.com/mbbennis/s3lf/internal/tui"
)

func main() {
	profile := flag.String("profile", "", "AWS profile name (defaults to AWS_PROFILE env or 'default')")
	region := flag.String("region", "", "Default AWS region (overrides profile)")
	readOnly := flag.Bool("read-only", false, "Disable destructive operations (delete, edit-save)")
	downloadDir := flag.String("download-dir", "", "Destination for downloads (default: cwd)")
	editSizeLimit := flag.Int64("edit-size-limit", 10*1024*1024, "Refuse to open files larger than this many bytes in $EDITOR")
	flag.Parse()

	// The starting profile is whatever the user passed on the CLI; if blank,
	// fall back to what the SDK would resolve so the header chip is accurate.
	initialProfile := *profile
	if initialProfile == "" {
		initialProfile = awscfg.Effective()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	cfg, err := awscfg.Load(ctx, *profile, *region)
	if err != nil {
		fmt.Fprintf(os.Stderr, "aws config: %v\n", err)
		os.Exit(1)
	}

	fs := s3fs.NewClient(cfg)
	c := cache.New(fs, 256, 60*time.Second)

	// switcher rebuilds per-profile state on demand. It does its own short
	// timeout for credential resolution — separate from the foreground ctx.
	switcher := func(name string) (*cache.Cache, s3fs.FS, error) {
		sctx, scancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer scancel()
		cfg2, err := awscfg.Load(sctx, name, *region)
		if err != nil {
			return nil, nil, err
		}
		fs2 := s3fs.NewClient(cfg2)
		c2 := cache.New(fs2, 256, 60*time.Second)
		return c2, fs2, nil
	}

	app := tui.New(c, fs, tui.Options{
		ReadOnly:      *readOnly,
		DownloadDir:   *downloadDir,
		EditSizeLimit: *editSizeLimit,
		Profile:       initialProfile,
		SwitchProfile: switcher,
	})

	p := tea.NewProgram(app, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		os.Exit(1)
	}
}
