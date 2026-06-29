package plex

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

const sectionsJSON = `{
  "MediaContainer": {
    "Directory": [
      {"key": "1", "title": "Films", "type": "movie"},
      {"key": "2", "title": "Séries", "type": "show"}
    ]
  }
}`

const recentlyAddedJSON = `{
  "MediaContainer": {
    "Metadata": [
      {"ratingKey": "101", "guid": "plex://movie/abc", "type": "movie", "title": "Dune", "summary": "Sci-fi", "year": 2021, "thumb": "/library/metadata/101/thumb/1", "addedAt": 1700000000},
      {"ratingKey": "202", "type": "episode", "title": "Pilot", "grandparentTitle": "Show", "addedAt": 1700000100}
    ]
  }
}`

const identityJSON = `{"MediaContainer": {"machineIdentifier": "machine-xyz"}}`

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/library/sections", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Plex-Token") == "" {
			t.Errorf("missing X-Plex-Token header")
		}
		_, _ = w.Write([]byte(sectionsJSON))
	})
	mux.HandleFunc("/library/sections/1/recentlyAdded", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("X-Plex-Container-Size"); got != "25" {
			t.Errorf("expected container size 25, got %q", got)
		}
		_, _ = w.Write([]byte(recentlyAddedJSON))
	})
	mux.HandleFunc("/identity", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(identityJSON))
	})
	return httptest.NewServer(mux)
}

func TestSections(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	c := NewClient(ts.URL, "tok", false)
	sections, err := c.Sections(context.Background())
	if err != nil {
		t.Fatalf("Sections: %v", err)
	}
	if len(sections) != 2 {
		t.Fatalf("expected 2 sections, got %d", len(sections))
	}
	if sections[0].Title != "Films" || sections[0].Key != "1" || sections[0].Type != "movie" {
		t.Errorf("unexpected first section: %+v", sections[0])
	}
}

func TestRecentlyAdded(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	c := NewClient(ts.URL, "tok", false)
	items, err := c.RecentlyAdded(context.Background(), "1", 25)
	if err != nil {
		t.Fatalf("RecentlyAdded: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].RatingKey != "101" || items[0].Title != "Dune" || items[0].Year != 2021 {
		t.Errorf("unexpected first item: %+v", items[0])
	}
	if items[0].AddedAt != 1700000000 {
		t.Errorf("unexpected addedAt: %d", items[0].AddedAt)
	}
	if items[1].GrandparentTitle != "Show" {
		t.Errorf("expected grandparentTitle Show, got %q", items[1].GrandparentTitle)
	}
}

func TestMachineIdentifier(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	c := NewClient(ts.URL, "tok", false)
	id, err := c.MachineIdentifier(context.Background())
	if err != nil {
		t.Fatalf("MachineIdentifier: %v", err)
	}
	if id != "machine-xyz" {
		t.Errorf("expected machine-xyz, got %q", id)
	}
}

func TestUnexpectedStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer ts.Close()

	c := NewClient(ts.URL, "tok", false)
	if _, err := c.Sections(context.Background()); err == nil {
		t.Fatal("expected error on non-200 status")
	}
}
