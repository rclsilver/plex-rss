package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rclsilver/plex-rss/internal/cache"
	"github.com/rclsilver/plex-rss/internal/plex"
)

const sectionsJSON = `{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"}]}}`
const allJSON = `{"MediaContainer":{"size":1,"totalSize":1,"Metadata":[{"ratingKey":"101","type":"movie","title":"Dune","year":2021,"addedAt":1700000000}]}}`
const identityJSON = `{"MediaContainer":{"machineIdentifier":"machine-xyz"}}`

func fakePlex(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/library/sections", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(sectionsJSON)) })
	mux.HandleFunc("/library/sections/1/all", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(allJSON)) })
	mux.HandleFunc("/identity", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(identityJSON)) })
	return httptest.NewServer(mux)
}

func newServer(t *testing.T) (*Server, *cache.Cache) {
	t.Helper()
	ts := fakePlex(t)
	t.Cleanup(ts.Close)

	pc := plex.NewClient(ts.URL, "plextok", false)
	c, err := cache.New(t.TempDir(), pc, "https://plex-rss.example.com", "feedtok", nil, func() time.Time { return time.Unix(1700001000, 0).UTC() })
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	return New(c, pc, "feedtok", "https://plex-rss.example.com"), c
}

func do(t *testing.T, h http.Handler, method, target string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestHealthNoToken(t *testing.T) {
	s, _ := newServer(t)
	rec := do(t, s.PublicHandler(), http.MethodGet, "/healthz")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestFeedRequiresToken(t *testing.T) {
	s, _ := newServer(t)
	rec := do(t, s.PublicHandler(), http.MethodGet, "/feed/1")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without token, got %d", rec.Code)
	}

	rec = do(t, s.PublicHandler(), http.MethodGet, "/feed/1?token=wrong")
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with wrong token, got %d", rec.Code)
	}
}

func TestColdCacheReturns503(t *testing.T) {
	s, _ := newServer(t)
	rec := do(t, s.PublicHandler(), http.MethodGet, "/feed/1?token=feedtok")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 on cold cache, got %d", rec.Code)
	}
}

func TestRefreshThenServe(t *testing.T) {
	s, _ := newServer(t)

	// Internal refresh of section 1.
	rec := do(t, s.InternalHandler(), http.MethodPost, "/refresh/1")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected refresh 200, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Feed is now served from cache.
	rec = do(t, s.PublicHandler(), http.MethodGet, "/feed/1?token=feedtok")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected feed 200, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/rss+xml") {
		t.Errorf("unexpected content-type: %q", ct)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Dune (2021)") {
		t.Errorf("expected feed to contain the movie, got: %s", body)
	}
}

func TestFeedsIndex(t *testing.T) {
	s, _ := newServer(t)
	rec := do(t, s.PublicHandler(), http.MethodGet, "/feeds?token=feedtok")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"title":"Films"`) || !strings.Contains(body, "/feed/1") {
		t.Errorf("unexpected feeds index: %s", body)
	}
}

func TestRefreshAll(t *testing.T) {
	s, c := newServer(t)
	if err := c.RefreshAll(context.Background()); err != nil {
		t.Fatalf("RefreshAll: %v", err)
	}
	_ = s
	if _, err := c.Read("1"); err != nil {
		t.Fatalf("expected section 1 cached after RefreshAll: %v", err)
	}
}

func TestAllowlistRestrictsSections(t *testing.T) {
	// Fake Plex with two libraries.
	mux := http.NewServeMux()
	mux.HandleFunc("/library/sections", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MediaContainer":{"Directory":[{"key":"1","title":"Films","type":"movie"},{"key":"2","title":"Séries","type":"show"}]}}`))
	})
	mux.HandleFunc("/library/sections/2/all", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":1,"totalSize":1,"Metadata":[{"ratingKey":"7","type":"episode","title":"Pilot","grandparentTitle":"Show","parentIndex":1,"index":1,"addedAt":1700000000}]}}`))
	})
	mux.HandleFunc("/identity", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(identityJSON)) })
	mux.HandleFunc("/library/sections/1/all", func(w http.ResponseWriter, _ *http.Request) {
		t.Error("section content must not be fetched for a non-allowed section")
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	pc := plex.NewClient(ts.URL, "plextok", false)
	// Allow only "Séries" (by title).
	c, err := cache.New(t.TempDir(), pc, "https://x", "feedtok", []string{"Séries"}, func() time.Time { return time.Unix(1, 0) })
	if err != nil {
		t.Fatalf("cache.New: %v", err)
	}
	s := New(c, pc, "feedtok", "https://x")

	// /feeds lists only the allowed section.
	rec := do(t, s.PublicHandler(), http.MethodGet, "/feeds?token=feedtok")
	body := rec.Body.String()
	if strings.Contains(body, `"title":"Films"`) {
		t.Errorf("Films must not be listed: %s", body)
	}
	if !strings.Contains(body, `"title":"Séries"`) {
		t.Errorf("Séries should be listed: %s", body)
	}

	// Refresh of a non-allowed section is rejected (404), file never created.
	rec = do(t, s.InternalHandler(), http.MethodPost, "/refresh/1")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 refreshing non-allowed section, got %d", rec.Code)
	}

	// Allowed section refresh works.
	rec = do(t, s.InternalHandler(), http.MethodPost, "/refresh/2")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 refreshing allowed section, got %d (%s)", rec.Code, rec.Body.String())
	}

	// Feed of the non-allowed section returns 404 (not 503), now that the
	// section list is known.
	rec = do(t, s.PublicHandler(), http.MethodGet, "/feed/1?token=feedtok")
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for non-allowed feed, got %d", rec.Code)
	}

	// Allowed feed is served.
	rec = do(t, s.PublicHandler(), http.MethodGet, "/feed/2?token=feedtok")
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 for allowed feed, got %d", rec.Code)
	}
}

func TestThumbProxy(t *testing.T) {
	// A fake Plex that also serves a thumbnail.
	mux := http.NewServeMux()
	mux.HandleFunc("/library/metadata/101/thumb/1", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("X-Plex-Token") != "plextok" {
			t.Errorf("thumb request missing token")
		}
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = io.WriteString(w, "JPEGDATA")
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	pc := plex.NewClient(ts.URL, "plextok", false)
	c, _ := cache.New(t.TempDir(), pc, "", "feedtok", nil, nil)
	s := New(c, pc, "feedtok", "")

	rec := do(t, s.PublicHandler(), http.MethodGet, "/thumb?token=feedtok&path=/library/metadata/101/thumb/1")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 from thumb proxy, got %d", rec.Code)
	}
	if rec.Body.String() != "JPEGDATA" {
		t.Errorf("unexpected thumb body: %q", rec.Body.String())
	}

	// Rejects non-relative paths.
	rec = do(t, s.PublicHandler(), http.MethodGet, "/thumb?token=feedtok&path=http://evil")
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for absolute path, got %d", rec.Code)
	}
}
