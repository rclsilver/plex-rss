// Package cache owns the pre-generated RSS files. RSS readers are only ever
// served from these files, so Plex is never hit on a feed read. Generation
// happens on three triggers: warm at startup, a periodic TTL refresh, and the
// internal /refresh route called by Sonarr/Radarr.
package cache

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rclsilver/plex-rss/internal/feed"
	"github.com/rclsilver/plex-rss/internal/plex"
)

// ErrNotCached is returned by Read when no cache file exists yet for a section.
var ErrNotCached = errors.New("section not cached yet")

// ErrSectionNotAllowed is returned when a section is not in the publish
// allowlist (or does not exist).
var ErrSectionNotAllowed = errors.New("section not allowed")

// Clock returns the current time; injectable for tests.
type Clock func() time.Time

// Cache coordinates Plex fetches, RSS rendering and atomic writes to disk.
type Cache struct {
	dir       string
	plex      *plex.Client
	publicURL string
	feedToken string
	now       Clock

	allow map[string]bool // lowercased allowed titles/keys; empty => allow all

	mu        sync.Mutex
	locks     map[string]*sync.Mutex
	machineID string
	machineOK bool
	sections  []plex.Section  // last fetched authorized sections
	authKeys  map[string]bool // authorized section keys (nil until first fetch)
}

// New creates a Cache and ensures the cache directory exists. allowSections is
// the publish allowlist (titles or keys, case-insensitive); empty publishes all
// libraries.
func New(dir string, p *plex.Client, publicURL, feedToken string, allowSections []string, now Clock) (*Cache, error) {
	if now == nil {
		now = time.Now
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	allow := map[string]bool{}
	for _, s := range allowSections {
		allow[strings.ToLower(strings.TrimSpace(s))] = true
	}
	return &Cache{
		dir:       dir,
		plex:      p,
		publicURL: publicURL,
		feedToken: feedToken,
		allow:     allow,
		now:       now,
		locks:     map[string]*sync.Mutex{},
	}, nil
}

// isAllowed reports whether a section is in the publish allowlist.
func (c *Cache) isAllowed(s plex.Section) bool {
	if len(c.allow) == 0 {
		return true
	}
	return c.allow[strings.ToLower(s.Key)] || c.allow[strings.ToLower(s.Title)]
}

// IsAuthorized reports whether the section key is authorized. known is false
// until the section list has been fetched at least once.
func (c *Cache) IsAuthorized(key string) (known, allowed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.authKeys == nil {
		return false, false
	}
	return true, c.authKeys[key]
}

func (c *Cache) lockFor(key string) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.locks[key]
	if !ok {
		l = &sync.Mutex{}
		c.locks[key] = l
	}
	return l
}

// Sections returns the authorized library sections, fetching them from Plex and
// caching the result in memory. Sections outside the allowlist are never
// returned, so they are never listed, generated or served.
func (c *Cache) Sections(ctx context.Context) ([]plex.Section, error) {
	all, err := c.plex.Sections(ctx)
	if err != nil {
		return nil, err
	}

	authorized := make([]plex.Section, 0, len(all))
	authKeys := make(map[string]bool, len(all))
	for _, s := range all {
		if c.isAllowed(s) {
			authorized = append(authorized, s)
			authKeys[s.Key] = true
		}
	}

	c.mu.Lock()
	c.sections = authorized
	c.authKeys = authKeys
	c.mu.Unlock()
	return authorized, nil
}

// resolveSection returns the authorized section for key, or ErrSectionNotAllowed
// if it is not in the allowlist (or does not exist).
func (c *Cache) resolveSection(ctx context.Context, key string) (plex.Section, error) {
	c.mu.Lock()
	cached := c.sections
	c.mu.Unlock()

	for _, s := range cached {
		if s.Key == key {
			return s, nil
		}
	}

	// Refresh the authorized section list and try again (handles a brand-new
	// library or a not-yet-populated cache).
	sections, err := c.Sections(ctx)
	if err != nil {
		return plex.Section{}, err
	}
	for _, s := range sections {
		if s.Key == key {
			return s, nil
		}
	}
	return plex.Section{}, ErrSectionNotAllowed
}

func (c *Cache) machineIdentifier(ctx context.Context) string {
	c.mu.Lock()
	if c.machineOK {
		id := c.machineID
		c.mu.Unlock()
		return id
	}
	c.mu.Unlock()

	id, err := c.plex.MachineIdentifier(ctx)
	if err != nil {
		// Deep links are best-effort; degrade gracefully.
		return ""
	}
	c.mu.Lock()
	c.machineID = id
	c.machineOK = true
	c.mu.Unlock()
	return id
}

// Refresh regenerates the cached feed for a single section from Plex.
func (c *Cache) Refresh(ctx context.Context, sectionKey string) error {
	lock := c.lockFor(sectionKey)
	lock.Lock()
	defer lock.Unlock()

	section, err := c.resolveSection(ctx, sectionKey)
	if err != nil {
		return err
	}

	// Show sections are published at the episode level; everything else (movies)
	// at the item level.
	items, err := c.plex.AllItems(ctx, sectionKey, section.Type == "show")
	if err != nil {
		return err
	}

	opts := feed.Options{
		SectionTitle: section.Title,
		SectionKey:   section.Key,
		SelfURL:      c.selfURL(section.Key),
		MachineID:    c.machineIdentifier(ctx),
		PublicURL:    c.publicURL,
		FeedToken:    c.feedToken,
	}

	body, err := feed.Build(items, opts, c.now())
	if err != nil {
		return err
	}
	return c.writeAtomic(sectionKey, body)
}

// RefreshAll regenerates every section's feed. It returns a combined error if
// any section fails, but still attempts all of them.
func (c *Cache) RefreshAll(ctx context.Context) error {
	sections, err := c.Sections(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, s := range sections {
		if err := c.Refresh(ctx, s.Key); err != nil {
			errs = append(errs, fmt.Errorf("section %s (%s): %w", s.Key, s.Title, err))
		}
	}
	return errors.Join(errs...)
}

// Read returns the cached RSS bytes for a section, or ErrNotCached if the feed
// has not been generated yet.
func (c *Cache) Read(sectionKey string) ([]byte, error) {
	body, err := os.ReadFile(c.pathFor(sectionKey))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotCached
	}
	return body, err
}

func (c *Cache) selfURL(sectionKey string) string {
	if c.publicURL == "" {
		return ""
	}
	u := strings.TrimRight(c.publicURL, "/") + "/feed/" + url.PathEscape(sectionKey)
	if c.feedToken != "" {
		u += "?token=" + url.QueryEscape(c.feedToken)
	}
	return u
}

func (c *Cache) pathFor(sectionKey string) string {
	return filepath.Join(c.dir, sanitize(sectionKey)+".xml")
}

func (c *Cache) writeAtomic(sectionKey string, body []byte) error {
	final := c.pathFor(sectionKey)
	tmp, err := os.CreateTemp(c.dir, ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed

	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, final)
}

// sanitize keeps only safe characters so a section key can never escape the
// cache directory.
func sanitize(key string) string {
	var b strings.Builder
	for _, r := range key {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "section"
	}
	return b.String()
}
