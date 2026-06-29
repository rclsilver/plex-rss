// Package feed renders Plex section content as an RSS 2.0 document. RSS is
// hand-rolled with encoding/xml to avoid any external dependency (keeps the
// scratch image and the build self-contained).
package feed

import (
	"encoding/xml"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/rclsilver/plex-rss/internal/plex"
)

// Options carries the data needed to render a feed that is independent of the
// items themselves.
type Options struct {
	SectionTitle string // library title, e.g. "Films"
	SectionKey   string // Plex section key
	SelfURL      string // public URL of this feed (for the atom:link self)
	MachineID    string // Plex server machine identifier (for deep links)
	PublicURL    string // base public URL of plex-rss (for the thumbnail proxy)
	FeedToken    string // feed token, embedded in thumbnail proxy URLs
}

type rss struct {
	XMLName xml.Name `xml:"rss"`
	Version string   `xml:"version,attr"`
	Atom    string   `xml:"xmlns:atom,attr"`
	Channel channel  `xml:"channel"`
}

type channel struct {
	Title         string    `xml:"title"`
	Link          string    `xml:"link"`
	Description   string    `xml:"description"`
	LastBuildDate string    `xml:"lastBuildDate,omitempty"`
	Generator     string    `xml:"generator"`
	AtomLink      *atomLink `xml:"atom:link,omitempty"`
	Items         []item    `xml:"item"`
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Type string `xml:"type,attr"`
}

type item struct {
	Title       string     `xml:"title"`
	Link        string     `xml:"link,omitempty"`
	GUID        guid       `xml:"guid"`
	PubDate     string     `xml:"pubDate,omitempty"`
	Description string     `xml:"description,omitempty"`
	Enclosure   *enclosure `xml:"enclosure,omitempty"`
}

type guid struct {
	Value       string `xml:",chardata"`
	IsPermaLink bool   `xml:"isPermaLink,attr"`
}

type enclosure struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

// Build renders the given items as an RSS 2.0 document. now is injected so the
// output is deterministic in tests.
func Build(items []plex.Item, opts Options, now time.Time) ([]byte, error) {
	ch := channel{
		Title:         feedTitle(opts.SectionTitle),
		Link:          channelLink(opts),
		Description:   fmt.Sprintf("Ajouts récents — %s", opts.SectionTitle),
		LastBuildDate: now.Format(time.RFC1123Z),
		Generator:     "plex-rss",
	}
	if opts.SelfURL != "" {
		ch.AtomLink = &atomLink{Href: opts.SelfURL, Rel: "self", Type: "application/rss+xml"}
	}

	for _, it := range items {
		entry := item{
			Title:       itemTitle(it),
			GUID:        guid{Value: it.GUID, IsPermaLink: false},
			Description: it.Summary,
		}
		if entry.GUID.Value == "" {
			entry.GUID.Value = it.RatingKey
		}
		if it.AddedAt > 0 {
			entry.PubDate = time.Unix(it.AddedAt, 0).UTC().Format(time.RFC1123Z)
		}
		if link := itemLink(it, opts); link != "" {
			entry.Link = link
		}
		if enc := thumbEnclosure(it, opts); enc != nil {
			entry.Enclosure = enc
		}
		ch.Items = append(ch.Items, entry)
	}

	doc := rss{
		Version: "2.0",
		Atom:    "http://www.w3.org/2005/Atom",
		Channel: ch,
	}

	body, err := xml.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, err
	}
	return append([]byte(xml.Header), body...), nil
}

func feedTitle(section string) string {
	if section == "" {
		return "Plex — ajouts récents"
	}
	return fmt.Sprintf("Plex — %s", section)
}

// itemTitle builds a human-friendly title:
//   - movies:   "Title (Year)"
//   - episodes: "Show — S01E02 — Title"
func itemTitle(it plex.Item) string {
	parts := []string{}
	if it.GrandparentTitle != "" {
		parts = append(parts, it.GrandparentTitle)
	}
	if it.Type == "episode" && it.ParentIndex > 0 && it.Index > 0 {
		parts = append(parts, fmt.Sprintf("S%02dE%02d", it.ParentIndex, it.Index))
	}
	title := it.Title
	if it.Type == "movie" && it.Year > 0 {
		title = fmt.Sprintf("%s (%d)", title, it.Year)
	}
	parts = append(parts, title)
	return strings.Join(parts, " — ")
}

func channelLink(opts Options) string {
	if opts.PublicURL != "" {
		return opts.PublicURL
	}
	return "https://app.plex.tv"
}

// itemLink builds an app.plex.tv deep link when the machine identifier is known.
func itemLink(it plex.Item, opts Options) string {
	if opts.MachineID == "" || it.RatingKey == "" {
		return ""
	}
	return fmt.Sprintf(
		"https://app.plex.tv/desktop/#!/server/%s/details?key=%s",
		opts.MachineID,
		url.QueryEscape("/library/metadata/"+it.RatingKey),
	)
}

// thumbEnclosure builds a link to the plex-rss thumbnail proxy so the Plex token
// is never exposed in the public feed. Requires PublicURL to be configured.
func thumbEnclosure(it plex.Item, opts Options) *enclosure {
	if it.Thumb == "" || opts.PublicURL == "" {
		return nil
	}
	q := url.Values{}
	q.Set("path", it.Thumb)
	if opts.FeedToken != "" {
		q.Set("token", opts.FeedToken)
	}
	return &enclosure{
		URL:  strings.TrimRight(opts.PublicURL, "/") + "/thumb?" + q.Encode(),
		Type: "image/jpeg",
	}
}
