# plex-rss

Generates **RSS** feeds from the content of a **Plex** media library: one feed per
library (Movies, TV Shows…), listing the **entire library** (full catalog), sorted
by date added (newest first). TV libraries are published **at the episode level**.

## Why a cache?

RSS readers poll feeds very frequently. To **avoid overloading the Plex server**,
`plex-rss` never contacts Plex when a feed is read: it serves a **pre-generated**
RSS file (one per library, in `CACHE_DIR`).

Regeneration from Plex is only triggered by:

1. **a warm-up at startup** — all libraries are generated at boot;
2. **a fallback TTL** — periodic refresh (`REFRESH_INTERVAL`, 6h by default) in
   case an event was missed;
3. **an internal refresh route** — `POST /refresh/{sectionKey}`, called by
   **Sonarr/Radarr** on import (via a "Webhook" connection).

The cache is ephemeral (rebuilt at startup); no persistent storage is required.

## Endpoints

### Public server (`SERVER_PORT`, default `8080`) — publicly exposed

All routes (except `/healthz`) require the feed token via `?token=…`.

| Method | Route | Description |
|---|---|---|
| GET | `/healthz` | Health check (no token). |
| GET | `/feeds?token=…` | JSON index of the libraries and their feed URLs. |
| GET | `/feed/{sectionKey}?token=…` | RSS feed of a library (served from the cache; `503` while the cache is cold). |
| GET | `/thumb?token=…&path=…` | Thumbnail proxy (avoids exposing the Plex token in the feed). |

### Internal server (`INTERNAL_PORT`, default `8081`) — **ClusterIP only**

| Method | Route | Description |
|---|---|---|
| POST | `/refresh/{sectionKey}` | Regenerates a library's feed from Plex. |
| POST | `/refresh/all` | Regenerates all libraries. |

This server has **no authentication** and must never be exposed through the
Ingress: it relies on in-cluster network isolation.

## Configuration (environment variables)

| Variable | Required | Default | Description |
|---|---|---|---|
| `PLEX_URL` | ✅ | — | Plex server URL (e.g. `http://plex:32400`). |
| `PLEX_TOKEN` | ✅ | — | Plex API token (`X-Plex-Token`). |
| `PLEX_INSECURE` | | `false` | Skip TLS verification (if `PLEX_URL` is https). |
| `FEED_TOKEN` | ✅ | — | Token expected in `?token=` to serve a feed. |
| `SECTIONS` | | _(all)_ | Allowlist of libraries to publish, comma-separated, by **exact title or key** (case-insensitive). E.g. `Movies,TV Shows`. Empty = all. |
| `PUBLIC_URL` | | — | Public base URL (`self` links, thumbnail URLs). |
| `CACHE_DIR` | | `/cache` | Directory of the pre-generated RSS files. |
| `REFRESH_INTERVAL` | | `6h` | Fallback refresh TTL. |
| `SERVER_PORT` | | `8080` | Public server port. |
| `INTERNAL_PORT` | | `8081` | Internal server port. |

## Development

```sh
make build      # build the ./plex-rss binary
make test       # unit tests
make vet        # go vet
make run        # build + run
```

Run locally:

```sh
export PLEX_URL=http://localhost:32400
export PLEX_TOKEN=…           # X-Plex-Token
export FEED_TOKEN=test
./plex-rss
# then:
curl localhost:8080/healthz
curl 'localhost:8080/feeds?token=test'
curl -X POST localhost:8081/refresh/all
curl 'localhost:8080/feed/1?token=test'
```

## Sonarr / Radarr configuration

In **Settings → Connect → Webhook**, triggers "On Import" (+ "On Upgrade"), URL:

```
http://plex-rss.mediacenter.svc.cluster.local:8081/refresh/<sectionKey>
```

(Sonarr → TV Shows section, Radarr → Movies section; the `sectionKey` values can be
read from `GET /feeds`.)

## Docker

```sh
docker build -t plex-rss .
docker run --rm -p 8080:8080 -p 8081:8081 \
  -e PLEX_URL=http://plex:32400 -e PLEX_TOKEN=… -e FEED_TOKEN=… \
  -v "$(pwd)/cache:/cache" \
  plex-rss
```

The published image is `ghcr.io/rclsilver/plex-rss`.
