package tui

import (
	"context"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/mbbennis/s3lf/internal/browser"
	"github.com/mbbennis/s3lf/internal/cache"
	"github.com/mbbennis/s3lf/internal/s3fs"
)

// App is the top-level Bubble Tea model.
type App struct {
	cache   *cache.Cache
	fs      s3fs.FS // used by ops; cache wraps the same FS for reads
	model   browser.Model
	width   int
	height  int
	status  string
	lastKey string // for two-key sequences like "gg"

	// pendingEdit holds in-flight edit state across the Exec hop (which is
	// the only point Bubble Tea returns control to Update before our work
	// is "done"). Nil when no edit is in progress.
	pendingEdit *pendingEdit

	// Prompt: while non-nil, every keystroke goes to the prompt instead of
	// the normal keymap. Each call site (search, delete-confirm, etc.)
	// supplies its own onChange/onSubmit/onCancel.
	prompt *prompt

	// Search state retained across prompts. committed drives n/N; savedCursor
	// restores the cursor when the search prompt is cancelled or backspaced
	// to empty.
	committed   string
	savedCursor int

	// Read-only mode disables D (delete) and e (edit). Surfaced via [RO] in
	// the header.
	readOnly bool

	// help mode toggles a full-screen keybinding reference.
	help bool

	// picker is non-nil while a modal list-picker is active.
	picker *picker

	// profile is the AWS profile this App is bound to (header chip + status).
	profile string

	// switchProfile rebuilds fs+cache for a new profile name. Injected by
	// main.go so the TUI doesn't pull in the AWS SDK or awscfg directly.
	switchProfile SwitchProfileFunc

	// Default destination for downloads. Empty = cwd.
	downloadDir string

	// Edit-size limit in bytes. Files larger than this refuse to open in
	// the editor.
	editSizeLimit int64

	// inflight tracks which fetches we've already kicked off so the same
	// missing-listing in two panes doesn't fire two fetches in the same tick.
	inflight     map[cache.Key]bool
	inflightNext map[cache.Key]bool
}

// paginateThreshold: trigger next-page fetch when the cursor is within this
// many rows of the loaded end. ListObjectsV2 returns up to 1000 entries per
// call, so a 50-row buffer keeps scroll smooth without burning calls.
const paginateThreshold = 50

// SwitchProfileFunc rebuilds the cache+fs for the named profile. Returns
// the new pair (and the resolved profile name, in case the caller does any
// normalization) or an error if the profile couldn't be loaded.
type SwitchProfileFunc func(name string) (*cache.Cache, s3fs.FS, error)

// Options configures App at construction time.
type Options struct {
	ReadOnly      bool
	DownloadDir   string
	EditSizeLimit int64

	// Profile is the AWS profile currently bound. Shown in the header.
	Profile string
	// SwitchProfile rebuilds fs+cache for a chosen profile. If nil, the
	// in-session profile picker is disabled (P key shows a status message).
	SwitchProfile SwitchProfileFunc
}

func New(c *cache.Cache, fs s3fs.FS, opts Options) *App {
	return &App{
		cache:         c,
		fs:            fs,
		model:         browser.New(),
		inflight:      map[cache.Key]bool{},
		inflightNext:  map[cache.Key]bool{},
		readOnly:      opts.ReadOnly,
		downloadDir:   opts.DownloadDir,
		editSizeLimit: opts.EditSizeLimit,
		profile:       opts.Profile,
		switchProfile: opts.SwitchProfile,
	}
}

// pendingEdit holds state across the tea.ExecProcess boundary. After the
// editor exits, the TUI message handler uses these fields to fire the
// upload-on-save Cmd.
type pendingEdit struct {
	bucket      string
	key         string
	path        string
	etag        string
	hash        string
	contentType string
}

func (a *App) Init() tea.Cmd {
	return a.ensureLoaded()
}

// --- messages ---

type bucketsLoadedMsg struct {
	buckets []s3fs.Bucket
	err     error
}

type listingLoadedMsg struct {
	key     cache.Key
	listing *s3fs.Listing
	err     error
}

type nextPageLoadedMsg struct {
	key cache.Key
	err error
}

// --- Update ---

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		return a, nil

	case tea.KeyMsg:
		if a.help {
			return a.handleHelpKey(msg)
		}
		if a.picker != nil {
			return a.handlePickerKey(msg)
		}
		if a.prompt != nil {
			return a.handlePromptKey(msg)
		}
		return a.handleKey(msg)

	case bucketsLoadedMsg:
		if msg.err != nil {
			a.status = "list buckets: " + msg.err.Error()
		}
		return a, a.ensureLoaded()

	case listingLoadedMsg:
		delete(a.inflight, msg.key)
		if msg.err != nil {
			a.status = "list: " + msg.err.Error()
		}
		// Re-clamp cursor if it ran past the end of the freshly loaded page.
		a.model.MoveCursor(a.cache, 0)
		return a, a.ensureLoaded()

	case nextPageLoadedMsg:
		delete(a.inflightNext, msg.key)
		if msg.err != nil {
			a.status = "page: " + msg.err.Error()
		}
		return a, a.ensureLoaded()
	}
	if cmd := a.handleOpsMsg(msg); cmd != nil {
		return a, cmd
	}
	return a, nil
}

func (a *App) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	prev := a.lastKey
	a.lastKey = msg.String()

	switch {
	case matchKey(msg, "q", "ctrl+c"):
		return a, tea.Quit

	case matchKey(msg, "j", "down"):
		a.model.MoveCursor(a.cache, 1)
	case matchKey(msg, "k", "up"):
		a.model.MoveCursor(a.cache, -1)

	case matchKey(msg, "g"):
		if prev == "g" {
			a.model.GotoTop()
			a.lastKey = ""
		}
	case matchKey(msg, "G"):
		a.model.GotoBottom(a.cache)

	case matchKey(msg, "l", "right", "enter"):
		a.model.Enter(a.cache)

	case matchKey(msg, "h", "left", "backspace"):
		a.model.Back()

	case matchKey(msg, "R"):
		a.refreshCurrent()

	case matchKey(msg, "/"):
		a.openSearchPrompt()
	case matchKey(msg, "n"):
		a.jumpCommitted(true)
	case matchKey(msg, "N"):
		a.jumpCommitted(false)
	case matchKey(msg, "esc"):
		a.committed = ""

	case matchKey(msg, "y"):
		if cmd := a.triggerDownload(); cmd != nil {
			return a, tea.Batch(cmd, a.ensureLoaded())
		}
	case matchKey(msg, "v"):
		if cmd := a.triggerView(); cmd != nil {
			return a, tea.Batch(cmd, a.ensureLoaded())
		}
	case matchKey(msg, "o"):
		if cmd := a.triggerOpen(); cmd != nil {
			return a, tea.Batch(cmd, a.ensureLoaded())
		}
	case matchKey(msg, "e"):
		if cmd := a.triggerEdit(); cmd != nil {
			return a, tea.Batch(cmd, a.ensureLoaded())
		}
	case matchKey(msg, "D"):
		if cmd := a.triggerDelete(); cmd != nil {
			return a, tea.Batch(cmd, a.ensureLoaded())
		}

	case matchKey(msg, "?"):
		a.help = true

	case matchKey(msg, "P"):
		a.openProfilePicker()
	}

	return a, a.ensureLoaded()
}

// handleHelpKey closes the help screen on ?, Esc, or q. All other keys
// are swallowed so the help screen is modal.
func (a *App) handleHelpKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if matchKey(msg, "?", "esc", "q") {
		a.help = false
	}
	return a, nil
}

// handlePromptKey routes keystrokes to a.prompt's callbacks.
func (a *App) handlePromptKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := a.prompt
	var cmd tea.Cmd
	switch {
	case matchKey(msg, "esc", "ctrl+c"):
		a.prompt = nil
		if p.onCancel != nil {
			cmd = p.onCancel()
		}
	case matchKey(msg, "enter"):
		a.prompt = nil
		if p.onSubmit != nil {
			cmd = p.onSubmit(p.query)
		}
	case matchKey(msg, "backspace"):
		if len(p.query) > 0 {
			runes := []rune(p.query)
			p.query = string(runes[:len(runes)-1])
			if p.onChange != nil {
				cmd = p.onChange(p.query)
			}
		}
	default:
		if s := msg.String(); len(s) == 1 {
			p.query += s
			if p.onChange != nil {
				cmd = p.onChange(p.query)
			}
		}
	}
	return a, tea.Batch(cmd, a.ensureLoaded())
}

// openSearchPrompt builds the / search prompt: live-jump on every keystroke,
// commit on Enter (saved for n/N), restore cursor on Esc.
func (a *App) openSearchPrompt() {
	a.savedCursor = a.model.Current().Cursor
	a.prompt = &prompt{
		label: "/",
		onChange: func(q string) tea.Cmd {
			if q == "" {
				a.model.Current().Cursor = a.savedCursor
				return nil
			}
			if idx, ok := a.findMatch(q, a.savedCursor-1, true); ok {
				a.model.Current().Cursor = idx
			}
			return nil
		},
		onSubmit: func(q string) tea.Cmd {
			a.committed = q
			return nil
		},
		onCancel: func() tea.Cmd {
			a.model.Current().Cursor = a.savedCursor
			return nil
		},
	}
}

func (a *App) jumpCommitted(forward bool) {
	if a.committed == "" {
		return
	}
	if idx, ok := a.findMatch(a.committed, a.model.Current().Cursor, forward); ok {
		a.model.Current().Cursor = idx
	}
}

// --- side effects ---

// ensureLoaded returns a tea.Cmd that fires any fetches needed for the
// currently visible panes (parent, current, preview).
func (a *App) ensureLoaded() tea.Cmd {
	var cmds []tea.Cmd

	// Bucket list — always needed; the root pane renders it.
	if _, ok := a.cache.PeekBuckets(); !ok {
		cmds = append(cmds, a.fetchBuckets())
	}

	visit := func(p s3fs.Path) {
		if p.IsRoot() {
			return
		}
		k := cache.Key{Bucket: p.Bucket, Prefix: p.Prefix}
		if _, ok := a.cache.Get(k); ok {
			return
		}
		if a.inflight[k] {
			return
		}
		a.inflight[k] = true
		cmds = append(cmds, a.fetchListing(k))
	}

	if par := a.model.Parent(); par != nil {
		visit(par.Path)
	}
	visit(a.model.Current().Path)
	if preview, ok := a.model.PreviewPath(a.cache); ok {
		visit(preview)
	}

	// Pagination: if the cursor on the current pane is near the loaded end
	// and the listing has more pages, kick off the next-page fetch.
	if cmd := a.maybePaginate(); cmd != nil {
		cmds = append(cmds, cmd)
	}

	if len(cmds) == 0 {
		return nil
	}
	return tea.Batch(cmds...)
}

// maybePaginate returns a Cmd to fetch the next page of the current pane's
// listing if the cursor is near the loaded end and more pages exist.
func (a *App) maybePaginate() tea.Cmd {
	cur := a.model.Current()
	if cur.Path.IsRoot() {
		return nil
	}
	k := cache.Key{Bucket: cur.Path.Bucket, Prefix: cur.Path.Prefix}
	l, ok := a.cache.Get(k)
	if !ok || l.Complete {
		return nil
	}
	if a.inflightNext[k] {
		return nil
	}
	if cur.Cursor < len(l.Entries)-paginateThreshold {
		return nil
	}
	a.inflightNext[k] = true
	return a.fetchNextPage(k)
}

func (a *App) fetchNextPage(k cache.Key) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := a.cache.FetchNext(ctx, k)
		return nextPageLoadedMsg{key: k, err: err}
	}
}

func (a *App) fetchBuckets() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		bs, err := a.cache.Buckets(ctx)
		return bucketsLoadedMsg{buckets: bs, err: err}
	}
}

func (a *App) fetchListing(k cache.Key) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		l, err := a.cache.Fetch(ctx, k)
		return listingLoadedMsg{key: k, listing: l, err: err}
	}
}

func (a *App) refreshCurrent() {
	cur := a.model.Current().Path
	if cur.IsRoot() {
		a.cache.InvalidateBuckets()
		return
	}
	a.cache.Invalidate(cache.Key{Bucket: cur.Bucket, Prefix: cur.Prefix})
}
