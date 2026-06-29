// Package server exposes two independent HTTP handlers:
//   - Public: /healthz, /feeds, /feed/{key}, /thumb — token-gated, served from
//     the cache, exposed through the Ingress.
//   - Internal: /refresh/{key} — no auth, ClusterIP only, called by Sonarr/Radarr.
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"

	"github.com/rclsilver/plex-rss/internal/cache"
	"github.com/rclsilver/plex-rss/internal/plex"
)

// Server wires the cache and Plex client into HTTP handlers.
type Server struct {
	cache     *cache.Cache
	plex      *plex.Client
	feedToken string
	publicURL string
}

// New builds a Server.
func New(c *cache.Cache, p *plex.Client, feedToken, publicURL string) *Server {
	return &Server{cache: c, plex: p, feedToken: feedToken, publicURL: publicURL}
}

// PublicHandler returns the handler for the public (Ingress-exposed) listener.
func (s *Server) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("GET /feeds", s.requireToken(s.handleFeeds))
	mux.HandleFunc("GET /feed/{key}", s.requireToken(s.handleFeed))
	mux.HandleFunc("GET /thumb", s.requireToken(s.handleThumb))
	return mux
}

// InternalHandler returns the handler for the internal (ClusterIP-only) listener.
func (s *Server) InternalHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /refresh/{key}", s.handleRefresh)
	return mux
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "ok")
}

// requireToken enforces a constant-time comparison of the ?token= query against
// the configured feed token.
func (s *Server) requireToken(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got := r.URL.Query().Get("token")
		if subtle.ConstantTimeCompare([]byte(got), []byte(s.feedToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

type feedInfo struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Type  string `json:"type"`
	URL   string `json:"url"`
}

func (s *Server) handleFeeds(w http.ResponseWriter, r *http.Request) {
	sections, err := s.cache.Sections(r.Context())
	if err != nil {
		log.Printf("feeds: list sections: %v", err)
		http.Error(w, "failed to list sections", http.StatusBadGateway)
		return
	}

	out := make([]feedInfo, 0, len(sections))
	for _, sec := range sections {
		out = append(out, feedInfo{
			Key:   sec.Key,
			Title: sec.Title,
			Type:  sec.Type,
			URL:   s.feedURL(sec.Key),
		})
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"feeds": out})
}

func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	// Refuse sections outside the allowlist outright (404) once we know the
	// section list, so unauthorized libraries are never even hinted at.
	if known, allowed := s.cache.IsAuthorized(key); known && !allowed {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	body, err := s.cache.Read(key)
	if errors.Is(err, cache.ErrNotCached) {
		w.Header().Set("Retry-After", "30")
		http.Error(w, "feed not generated yet, retry shortly", http.StatusServiceUnavailable)
		return
	}
	if err != nil {
		log.Printf("feed %s: read cache: %v", key, err)
		http.Error(w, "failed to read feed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	_, _ = w.Write(body)
}

// handleThumb proxies a Plex thumbnail so the Plex token is never exposed in the
// public feed. The thumbnail path is taken from the ?path= query and must be a
// Plex-relative path.
func (s *Server) handleThumb(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	if path == "" || !strings.HasPrefix(path, "/") {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}

	q := url.Values{}
	q.Set("X-Plex-Token", s.plex.Token())
	target := s.plex.BaseURL() + path + "?" + q.Encode()

	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, target, nil)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	resp, err := s.plex.HTTPClient().Do(req)
	if err != nil {
		log.Printf("thumb %s: %v", path, err)
		http.Error(w, "upstream error", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		http.Error(w, "thumbnail not found", http.StatusNotFound)
		return
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = io.Copy(w, resp.Body)
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")

	var err error
	if key == "all" {
		err = s.cache.RefreshAll(r.Context())
	} else {
		err = s.cache.Refresh(r.Context(), key)
	}
	if errors.Is(err, cache.ErrSectionNotAllowed) {
		http.Error(w, "section not allowed", http.StatusNotFound)
		return
	}
	if err != nil {
		log.Printf("refresh %s: %v", key, err)
		http.Error(w, "refresh failed: "+err.Error(), http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "refreshed "+key)
}

func (s *Server) feedURL(sectionKey string) string {
	if s.publicURL == "" {
		return "/feed/" + url.PathEscape(sectionKey)
	}
	u := strings.TrimRight(s.publicURL, "/") + "/feed/" + url.PathEscape(sectionKey)
	if s.feedToken != "" {
		u += "?token=" + url.QueryEscape(s.feedToken)
	}
	return u
}
