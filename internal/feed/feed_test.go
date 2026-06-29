package feed

import (
	"encoding/xml"
	"strings"
	"testing"
	"time"

	"github.com/rclsilver/plex-rss/internal/plex"
)

// parsedRSS is a minimal structure to validate the rendered XML.
type parsedRSS struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Channel struct {
		Title string `xml:"title"`
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			GUID        string `xml:"guid"`
			PubDate     string `xml:"pubDate"`
			Description string `xml:"description"`
			Enclosure   struct {
				URL  string `xml:"url,attr"`
				Type string `xml:"type,attr"`
			} `xml:"enclosure"`
		} `xml:"item"`
	} `xml:"channel"`
}

func sampleItems() []plex.Item {
	return []plex.Item{
		{RatingKey: "101", GUID: "plex://movie/abc", Type: "movie", Title: "Dune", Summary: "Sci-fi", Year: 2021, Thumb: "/library/metadata/101/thumb/1", AddedAt: 1700000000},
		{RatingKey: "202", Type: "episode", Title: "Pilot", GrandparentTitle: "Show", AddedAt: 1700000100},
	}
}

func buildAndParse(t *testing.T, items []plex.Item, opts Options) parsedRSS {
	t.Helper()
	body, err := Build(items, opts, time.Unix(1700001000, 0).UTC())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if !strings.HasPrefix(string(body), "<?xml") {
		t.Errorf("expected XML header, got: %s", string(body[:20]))
	}
	var parsed parsedRSS
	if err := xml.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("output is not valid XML: %v", err)
	}
	return parsed
}

func TestBuildBasic(t *testing.T) {
	opts := Options{
		SectionTitle: "Films",
		SectionKey:   "1",
		MachineID:    "machine-xyz",
		PublicURL:    "https://plex-rss.example.com",
		FeedToken:    "secret",
	}
	parsed := buildAndParse(t, sampleItems(), opts)

	if parsed.Version != "2.0" {
		t.Errorf("expected RSS 2.0, got %q", parsed.Version)
	}
	if parsed.Channel.Title != "Plex — Films" {
		t.Errorf("unexpected channel title: %q", parsed.Channel.Title)
	}
	if len(parsed.Channel.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(parsed.Channel.Items))
	}

	first := parsed.Channel.Items[0]
	if first.Title != "Dune (2021)" {
		t.Errorf("expected movie title with year, got %q", first.Title)
	}
	if first.GUID != "plex://movie/abc" {
		t.Errorf("unexpected guid: %q", first.GUID)
	}
	if !strings.Contains(first.Link, "machine-xyz") {
		t.Errorf("expected deep link with machine id, got %q", first.Link)
	}
	if first.PubDate == "" {
		t.Error("expected a pubDate")
	}
	if !strings.Contains(first.Enclosure.URL, "/thumb?") || !strings.Contains(first.Enclosure.URL, "token=secret") {
		t.Errorf("expected thumbnail proxy enclosure, got %q", first.Enclosure.URL)
	}

	// Episode title is prefixed with its show.
	if parsed.Channel.Items[1].Title != "Show — Pilot" {
		t.Errorf("expected episode title 'Show — Pilot', got %q", parsed.Channel.Items[1].Title)
	}
}

func TestBuildWithoutPublicURLHasNoEnclosure(t *testing.T) {
	parsed := buildAndParse(t, sampleItems(), Options{SectionTitle: "Films"})
	if parsed.Channel.Items[0].Enclosure.URL != "" {
		t.Errorf("expected no enclosure without PublicURL, got %q", parsed.Channel.Items[0].Enclosure.URL)
	}
	if parsed.Channel.Items[0].Link != "" {
		t.Errorf("expected no deep link without machine id, got %q", parsed.Channel.Items[0].Link)
	}
}
