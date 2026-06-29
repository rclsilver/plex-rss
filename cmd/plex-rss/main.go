package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rclsilver/plex-rss/internal/cache"
	"github.com/rclsilver/plex-rss/internal/config"
	"github.com/rclsilver/plex-rss/internal/plex"
	"github.com/rclsilver/plex-rss/internal/server"
	"github.com/rclsilver/plex-rss/internal/version"
)

func main() {
	log.Printf("Starting plex-rss %s", version.VersionFull())

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Configuration loaded:")
	log.Printf("  Plex URL:         %s", cfg.PlexURL)
	log.Printf("  Cache dir:        %s", cfg.CacheDir)
	log.Printf("  Refresh interval: %s", cfg.RefreshInterval)
	log.Printf("  Public port:      %d", cfg.ServerPort)
	log.Printf("  Internal port:    %d", cfg.InternalPort)
	if len(cfg.Sections) == 0 {
		log.Printf("  Sections:         <all>")
	} else {
		log.Printf("  Sections:         %v", cfg.Sections)
	}

	plexClient := plex.NewClient(cfg.PlexURL, cfg.PlexToken, cfg.PlexInsecure)

	c, err := cache.New(cfg.CacheDir, plexClient, cfg.PublicURL, cfg.FeedToken, cfg.Sections, time.Now)
	if err != nil {
		log.Fatalf("Failed to initialize cache: %v", err)
	}

	srv := server.New(c, plexClient, cfg.FeedToken, cfg.PublicURL)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Warm the cache at startup (best-effort, non-blocking so the listeners and
	// health checks come up immediately).
	go func() {
		log.Printf("Warming cache...")
		if err := c.RefreshAll(ctx); err != nil {
			log.Printf("Initial cache warm completed with errors: %v", err)
		} else {
			log.Printf("Cache warmed")
		}
	}()

	// Periodic TTL refresh as a safety net for missed webhooks.
	go refreshLoop(ctx, c, cfg.RefreshInterval)

	internal := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.InternalPort),
		Handler: srv.InternalHandler(),
	}
	public := &http.Server{
		Addr:    fmt.Sprintf(":%d", cfg.ServerPort),
		Handler: srv.PublicHandler(),
	}

	go func() {
		log.Printf("Internal server listening on %s", internal.Addr)
		if err := internal.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Internal server failed: %v", err)
		}
	}()

	go func() {
		log.Printf("Public server listening on %s", public.Addr)
		if err := public.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Public server failed: %v", err)
		}
	}()

	<-ctx.Done()
	log.Printf("Shutting down...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = public.Shutdown(shutdownCtx)
	_ = internal.Shutdown(shutdownCtx)
	os.Exit(0)
}

func refreshLoop(ctx context.Context, c *cache.Cache, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			log.Printf("TTL refresh triggered")
			if err := c.RefreshAll(ctx); err != nil {
				log.Printf("TTL refresh completed with errors: %v", err)
			}
		}
	}
}
