package cli

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/chrom-ar/hkr-cli/internal/api"
	"github.com/chrom-ar/hkr-cli/internal/auth"
	"github.com/chrom-ar/hkr-cli/internal/render"
	"github.com/chrom-ar/hkr-cli/internal/target"
)

// DefaultURL is the HKr used when nothing says which.
const DefaultURL = "https://app.hkr.chrom.ar"

// app is one run of hkr: its environment, global flags, and what
// it worked out along the way.
type app struct {
	env    Env
	flags  globalFlags
	logger *slog.Logger

	cfg       *auth.Config
	store     auth.TokenStore
	session   *session
	org       string // Once known, for error hints.
	domainArg string // The domain as typed, for error hints.
}

type globalFlags struct {
	url, org, lang                     string
	json, yes, quiet, verbose, noColor bool
}

// setup applies the global flags, once they are parsed.
func (a *app) setup() error {
	if a.env.Getenv("HKR_DEBUG") == "1" {
		a.flags.verbose = true
	}
	if a.env.Getenv("NO_COLOR") != "" {
		a.flags.noColor = true
	}
	level := slog.Level(100) // Off.
	if a.flags.verbose {
		level = slog.LevelDebug
	}
	a.logger = slog.New(slog.NewTextHandler(a.env.Stderr, &slog.HandlerOptions{Level: level}))
	return nil
}

func (a *app) dir() string { return filepath.Join(a.env.ConfigDir, "hkr") }

func (a *app) configPath() string { return filepath.Join(a.dir(), "config.json") }

// config is hkr's settings and where tokens are kept.
func (a *app) config() (*auth.Config, auth.TokenStore, error) {
	if a.cfg != nil {
		return a.cfg, a.store, nil
	}
	if a.env.ConfigDir == "" {
		return nil, nil, errors.New("can't find your config directory to keep the token in")
	}
	cfg, err := auth.LoadConfig(a.configPath())
	if err != nil {
		return nil, nil, err
	}
	warn := func(s string) { fmt.Fprintf(a.env.Stderr, "warning: %s\n", s) }
	file := &auth.FileStore{Path: filepath.Join(a.dir(), "tokens.json"), Warn: warn}
	store := &auth.Store{
		Keyring: a.env.Keyring,
		File:    file,
		Notice: func(path string) {
			fmt.Fprintf(a.env.Stderr, "No keychain found: the token is kept in %s, readable only by you.\n", path)
		},
	}
	a.cfg, a.store = &cfg, store
	return a.cfg, a.store, nil
}

// session is the HKr in use, the token for it and where it came
// from, and the language asked for.
type session struct {
	url     string
	token   api.Token
	fromEnv bool
	lang    string
}

// resolve works out the session: URL from --url, HKR_URL, the last
// login, the default; token from HKR_TOKEN, else the one stored
// for that URL (may be empty).
func (a *app) resolve() (*session, error) {
	if a.session != nil {
		return a.session, nil
	}
	s := &session{}
	lang := strings.TrimSpace(a.flags.lang)
	if lang == "" {
		lang = strings.TrimSpace(a.env.Getenv("HKR_LANG"))
	}
	if lang != "" && lang != "en" && lang != "es" {
		return nil, usageError{fmt.Errorf("unknown language %q: use en or es", lang)}
	}
	s.lang = lang

	cfg, store, err := a.config()
	if err != nil {
		return nil, err
	}
	raw := strings.TrimSpace(a.flags.url)
	if raw == "" {
		raw = strings.TrimSpace(a.env.Getenv("HKR_URL"))
	}
	switch {
	case raw != "":
		if s.url, err = api.OriginOf(raw); err != nil {
			return nil, usageError{err}
		}
	case cfg.URL != "":
		s.url = cfg.URL
	default:
		s.url = DefaultURL
	}

	if t := strings.TrimSpace(a.env.Getenv("HKR_TOKEN")); t != "" {
		s.token, s.fromEnv = api.Token(t), true
	} else {
		tok, err := store.Get(s.url)
		if err != nil && !errors.Is(err, auth.ErrNoToken) {
			return nil, err
		}
		s.token = tok
	}
	a.session = s
	return s, nil
}

// notSignedIn is the error for a HKr without a token.
type notSignedIn struct{ url string }

func (e notSignedIn) Error() string { return fmt.Sprintf("not signed in to %s.", e.url) }

func (a *app) userAgent() string {
	return fmt.Sprintf("hkr/%s (%s; %s)", a.env.Build.Version, runtime.GOOS, runtime.GOARCH)
}

// newClient returns an API client for url with tok (which may be empty).
func (a *app) newClient(url string, tok api.Token, lang string) (*api.Client, error) {
	c, err := api.New(api.Options{
		URL:        url,
		Token:      tok,
		Lang:       lang,
		UserAgent:  a.userAgent(),
		HTTPClient: a.env.HTTPClient,
		Logger:     a.logger,
		Sleep:      a.env.Sleep,
	})
	if err != nil {
		return nil, usageError{err}
	}
	return c, nil
}

// client is a signed-in client for the session.
func (a *app) client() (*api.Client, *session, error) {
	s, err := a.resolve()
	if err != nil {
		return nil, nil, err
	}
	if s.token == "" {
		return nil, nil, notSignedIn{s.url}
	}
	c, err := a.newClient(s.url, s.token, s.lang)
	return c, s, err
}

// resolver chooses the organization for c.
func (a *app) resolver(c *api.Client) *target.Resolver {
	r := &target.Resolver{API: c, Flag: a.flags.org, Env: a.env.Getenv("HKR_ORG")}
	if d, ok := a.defaultFor(c.Origin()); ok {
		r.Default = d.Org
	}
	return r
}

// inOrg is a signed-in client and the organization to work on.
func (a *app) inOrg(ctx context.Context) (*api.Client, string, error) {
	c, _, err := a.client()
	if err != nil {
		return nil, "", err
	}
	org, err := a.resolver(c).Org(ctx)
	if err != nil {
		return nil, "", err
	}
	a.org = org
	return c, org, nil
}

// defaultFor is what hkr use chose for the HKr at url.
func (a *app) defaultFor(url string) (auth.Default, bool) {
	cfg, _, err := a.config()
	if err != nil || cfg.Defaults == nil {
		return auth.Default{}, false
	}
	d, ok := cfg.Defaults[url]
	return d, ok
}

// printJSON writes HKr's JSON as is.
func (a *app) printJSON(raw []byte) error { return render.JSON(a.env.Stdout, raw) }

// out writes a result line to stdout.
func (a *app) out(format string, args ...any) { fmt.Fprintf(a.env.Stdout, format+"\n", args...) }

// progress writes to stderr unless --quiet.
func (a *app) progress(format string, args ...any) {
	if !a.flags.quiet {
		fmt.Fprintf(a.env.Stderr, format+"\n", args...)
	}
}

// warn writes a warning to stderr, even with --quiet.
func (a *app) warn(format string, args ...any) {
	fmt.Fprintf(a.env.Stderr, "warning: "+format+"\n", args...)
}
