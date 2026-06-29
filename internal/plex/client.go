// Package plex provides a minimal client for the Plex Media Server HTTP API,
// limited to what plex-rss needs: listing libraries and their recently added
// items, and resolving the server machine identifier (for building deep links).
package plex

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to a Plex Media Server using an X-Plex-Token.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// Section is a Plex library section (e.g. Movies, TV Shows).
type Section struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Type  string `json:"type"`
}

// Item is a single piece of media recently added to a section.
type Item struct {
	RatingKey        string
	Title            string
	Summary          string
	Type             string
	Year             int
	Thumb            string
	AddedAt          int64
	GUID             string
	ParentTitle      string
	GrandparentTitle string
}

// NewClient builds a Plex client. baseURL is e.g. http://plex:32400.
func NewClient(baseURL, token string, insecure bool) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		http: &http.Client{
			Timeout:   30 * time.Second,
			Transport: transport,
		},
	}
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out interface{}) error {
	u := c.baseURL + path
	if query == nil {
		query = url.Values{}
	}
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Plex-Token", c.token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("plex request %s: %w", path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("plex request %s: unexpected status %d: %s", path, resp.StatusCode, strings.TrimSpace(string(body)))
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("plex request %s: decode: %w", path, err)
	}
	return nil
}

type sectionsResponse struct {
	MediaContainer struct {
		Directory []Section `json:"Directory"`
	} `json:"MediaContainer"`
}

// Sections returns the list of library sections.
func (c *Client) Sections(ctx context.Context) ([]Section, error) {
	var r sectionsResponse
	if err := c.get(ctx, "/library/sections", nil, &r); err != nil {
		return nil, err
	}
	return r.MediaContainer.Directory, nil
}

// rawItem mirrors the Plex JSON metadata payload. Plex encodes ratingKey as a
// string and the numeric fields as numbers; addedAt is a unix timestamp.
type rawItem struct {
	RatingKey        string `json:"ratingKey"`
	Title            string `json:"title"`
	Summary          string `json:"summary"`
	Type             string `json:"type"`
	Year             int    `json:"year"`
	Thumb            string `json:"thumb"`
	AddedAt          int64  `json:"addedAt"`
	GUID             string `json:"guid"`
	ParentTitle      string `json:"parentTitle"`
	GrandparentTitle string `json:"grandparentTitle"`
}

type metadataResponse struct {
	MediaContainer struct {
		Metadata []rawItem `json:"Metadata"`
	} `json:"MediaContainer"`
}

// RecentlyAdded returns the most recently added items of a section, newest first,
// capped at limit.
func (c *Client) RecentlyAdded(ctx context.Context, sectionKey string, limit int) ([]Item, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("X-Plex-Container-Start", "0")
		q.Set("X-Plex-Container-Size", strconv.Itoa(limit))
	}

	var r metadataResponse
	path := "/library/sections/" + url.PathEscape(sectionKey) + "/recentlyAdded"
	if err := c.get(ctx, path, q, &r); err != nil {
		return nil, err
	}

	items := make([]Item, 0, len(r.MediaContainer.Metadata))
	for _, m := range r.MediaContainer.Metadata {
		items = append(items, Item{
			RatingKey:        m.RatingKey,
			Title:            m.Title,
			Summary:          m.Summary,
			Type:             m.Type,
			Year:             m.Year,
			Thumb:            m.Thumb,
			AddedAt:          m.AddedAt,
			GUID:             m.GUID,
			ParentTitle:      m.ParentTitle,
			GrandparentTitle: m.GrandparentTitle,
		})
	}
	return items, nil
}

type identityResponse struct {
	MediaContainer struct {
		MachineIdentifier string `json:"machineIdentifier"`
	} `json:"MediaContainer"`
}

// MachineIdentifier returns the server's unique identifier, used to build
// app.plex.tv deep links.
func (c *Client) MachineIdentifier(ctx context.Context) (string, error) {
	var r identityResponse
	if err := c.get(ctx, "/identity", nil, &r); err != nil {
		return "", err
	}
	return r.MediaContainer.MachineIdentifier, nil
}

// BaseURL returns the configured Plex base URL (used by the thumbnail proxy).
func (c *Client) BaseURL() string {
	return c.baseURL
}

// Token returns the configured Plex token (used by the thumbnail proxy).
func (c *Client) Token() string {
	return c.token
}

// HTTPClient exposes the underlying HTTP client (used by the thumbnail proxy).
func (c *Client) HTTPClient() *http.Client {
	return c.http
}
