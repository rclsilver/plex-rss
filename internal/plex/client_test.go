package plex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
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

const allJSON = `{
  "MediaContainer": {
    "size": 2,
    "totalSize": 2,
    "Metadata": [
      {"ratingKey": "101", "guid": "plex://movie/abc", "type": "movie", "title": "Dune", "summary": "Sci-fi", "year": 2021, "thumb": "/library/metadata/101/thumb/1", "addedAt": 1700000000},
      {"ratingKey": "202", "type": "episode", "title": "Pilot", "grandparentTitle": "Show", "parentIndex": 1, "index": 2, "addedAt": 1700000100}
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
	mux.HandleFunc("/library/sections/1/all", func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Query().Get("sort"); got != "addedAt:desc" {
			t.Errorf("expected sort addedAt:desc, got %q", got)
		}
		if got := r.URL.Query().Get("X-Plex-Container-Size"); got != "200" {
			t.Errorf("expected container size 200, got %q", got)
		}
		_, _ = w.Write([]byte(allJSON))
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

func TestAllItems(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	c := NewClient(ts.URL, "tok", false)
	items, err := c.AllItems(context.Background(), "1", false)
	if err != nil {
		t.Fatalf("AllItems: %v", err)
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
	if items[1].GrandparentTitle != "Show" || items[1].ParentIndex != 1 || items[1].Index != 2 {
		t.Errorf("unexpected episode fields: %+v", items[1])
	}
}

func TestAllItemsEpisodesParam(t *testing.T) {
	gotType := ""
	mux := http.NewServeMux()
	mux.HandleFunc("/library/sections/2/all", func(w http.ResponseWriter, r *http.Request) {
		gotType = r.URL.Query().Get("type")
		_, _ = w.Write([]byte(`{"MediaContainer":{"size":0,"totalSize":0,"Metadata":[]}}`))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := NewClient(ts.URL, "tok", false)
	if _, err := c.AllItems(context.Background(), "2", true); err != nil {
		t.Fatalf("AllItems: %v", err)
	}
	if gotType != "4" {
		t.Errorf("expected type=4 for episodes, got %q", gotType)
	}
}

func TestAllItemsPagination(t *testing.T) {
	// 250 items across two pages (200 + 50).
	mux := http.NewServeMux()
	mux.HandleFunc("/library/sections/9/all", func(w http.ResponseWriter, r *http.Request) {
		start := r.URL.Query().Get("X-Plex-Container-Start")
		n := 200
		if start == "200" {
			n = 50
		}
		var b strings.Builder
		b.WriteString(`{"MediaContainer":{"size":`)
		b.WriteString(itoa(n))
		b.WriteString(`,"totalSize":250,"Metadata":[`)
		for i := 0; i < n; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			b.WriteString(`{"ratingKey":"x","type":"movie","title":"M","addedAt":1}`)
		}
		b.WriteString(`]}}`)
		_, _ = w.Write([]byte(b.String()))
	})
	ts := httptest.NewServer(mux)
	defer ts.Close()

	c := NewClient(ts.URL, "tok", false)
	items, err := c.AllItems(context.Background(), "9", false)
	if err != nil {
		t.Fatalf("AllItems: %v", err)
	}
	if len(items) != 250 {
		t.Fatalf("expected 250 items across pages, got %d", len(items))
	}
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

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
