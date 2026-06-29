package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the application configuration, loaded from environment variables.
type Config struct {
	// Plex Media Server access
	PlexURL      string
	PlexToken    string
	PlexInsecure bool

	// Feed serving
	FeedToken string
	PublicURL string

	// Sections is the allowlist of libraries to publish, matched by exact title
	// or key (case-insensitive). Empty means publish every library.
	Sections []string

	// Cache
	CacheDir        string
	RefreshInterval time.Duration

	// HTTP listeners
	ServerPort   int // public server (feeds, healthz), exposed via the Ingress
	InternalPort int // internal server (refresh), ClusterIP only
}

// LoadConfig loads and validates the configuration from environment variables.
func LoadConfig() (*Config, error) {
	cfg := &Config{
		PlexURL:         os.Getenv("PLEX_URL"),
		PlexToken:       os.Getenv("PLEX_TOKEN"),
		FeedToken:       os.Getenv("FEED_TOKEN"),
		PublicURL:       os.Getenv("PUBLIC_URL"),
		CacheDir:        getEnv("CACHE_DIR", "/cache"),
		RefreshInterval: 6 * time.Hour,
		ServerPort:      8080,
		InternalPort:    8081,
	}

	if cfg.PlexURL == "" {
		return nil, fmt.Errorf("PLEX_URL environment variable is required")
	}
	if cfg.PlexToken == "" {
		return nil, fmt.Errorf("PLEX_TOKEN environment variable is required")
	}
	if cfg.FeedToken == "" {
		return nil, fmt.Errorf("FEED_TOKEN environment variable is required")
	}

	if v := os.Getenv("REFRESH_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil {
			return nil, fmt.Errorf("invalid REFRESH_INTERVAL: %w", err)
		}
		cfg.RefreshInterval = d
	}

	if err := parseInt(os.Getenv("SERVER_PORT"), &cfg.ServerPort, "SERVER_PORT"); err != nil {
		return nil, err
	}
	if err := parseInt(os.Getenv("INTERNAL_PORT"), &cfg.InternalPort, "INTERNAL_PORT"); err != nil {
		return nil, err
	}

	if v := os.Getenv("PLEX_INSECURE"); v != "" {
		b, err := strconv.ParseBool(v)
		if err != nil {
			return nil, fmt.Errorf("invalid PLEX_INSECURE: %w", err)
		}
		cfg.PlexInsecure = b
	}

	cfg.Sections = splitList(os.Getenv("SECTIONS"))

	return cfg, nil
}

// splitList parses a comma-separated list, trimming spaces and dropping empties.
func splitList(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseInt(raw string, dst *int, name string) error {
	if raw == "" {
		return nil
	}
	v, err := strconv.Atoi(raw)
	if err != nil {
		return fmt.Errorf("invalid %s: %w", name, err)
	}
	*dst = v
	return nil
}
