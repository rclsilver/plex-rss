# plex-rss

Génère des flux **RSS** à partir du contenu d'une médiathèque **Plex** : un flux
par bibliothèque (Films, Séries, Musique…), listant les **ajouts récents**.

## Pourquoi un cache ?

Les lecteurs RSS interrogent un flux très fréquemment. Pour **ne pas surcharger
le serveur Plex**, `plex-rss` ne contacte jamais Plex au moment d'une lecture :
il sert un fichier RSS **pré-généré** (un par bibliothèque, dans `CACHE_DIR`).

La (re)génération depuis Plex n'est déclenchée que par :

1. **un warm au démarrage** — toutes les bibliothèques sont générées au boot ;
2. **un TTL de secours** — rafraîchissement périodique (`REFRESH_INTERVAL`, 6 h
   par défaut) au cas où un événement serait manqué ;
3. **une route interne de refresh** — `POST /refresh/{sectionKey}`, appelée par
   **Sonarr/Radarr** lors d'un import (connexion « Webhook »).

Le cache est éphémère (reconstruit au démarrage) ; aucun stockage persistant
n'est requis.

## Endpoints

### Serveur public (`SERVER_PORT`, défaut `8080`) — exposé publiquement

Toutes les routes (sauf `/healthz`) exigent le token de flux via `?token=…`.

| Méthode | Route | Description |
|---|---|---|
| GET | `/healthz` | Health check (sans token). |
| GET | `/feeds?token=…` | Index JSON des bibliothèques et de leurs URLs de flux. |
| GET | `/feed/{sectionKey}?token=…` | Flux RSS d'une bibliothèque (servi depuis le cache ; `503` tant que le cache est froid). |
| GET | `/thumb?token=…&path=…` | Proxy de vignette (évite d'exposer le token Plex dans le flux). |

### Serveur interne (`INTERNAL_PORT`, défaut `8081`) — **ClusterIP uniquement**

| Méthode | Route | Description |
|---|---|---|
| POST | `/refresh/{sectionKey}` | Régénère le flux d'une bibliothèque depuis Plex. |
| POST | `/refresh/all` | Régénère toutes les bibliothèques. |

Ce serveur n'a **pas d'authentification** et ne doit jamais être exposé via
l'Ingress : il repose sur l'isolation réseau intra-cluster.

## Configuration (variables d'environnement)

| Variable | Requis | Défaut | Description |
|---|---|---|---|
| `PLEX_URL` | ✅ | — | URL du serveur Plex (ex. `http://plex:32400`). |
| `PLEX_TOKEN` | ✅ | — | Token d'API Plex (`X-Plex-Token`). |
| `PLEX_INSECURE` | | `false` | Ignore la vérification TLS (si `PLEX_URL` en https). |
| `FEED_TOKEN` | ✅ | — | Token attendu dans `?token=` pour servir un flux. |
| `SECTIONS` | | _(toutes)_ | Allowlist des bibliothèques à publier, séparées par des virgules, par **titre exact ou clé** (insensible à la casse). Ex. `Films,Séries`. Vide = toutes. |
| `PUBLIC_URL` | | — | Base d'URL publique (liens `self`, URLs de vignettes). |
| `DEFAULT_LIMIT` | | `25` | Nombre d'items par flux. |
| `CACHE_DIR` | | `/cache` | Répertoire des fichiers RSS pré-générés. |
| `REFRESH_INTERVAL` | | `6h` | TTL de rafraîchissement de secours. |
| `SERVER_PORT` | | `8080` | Port du serveur public. |
| `INTERNAL_PORT` | | `8081` | Port du serveur interne. |

## Développement

```sh
make build      # compile le binaire ./plex-rss
make test       # tests unitaires
make vet        # go vet
make run        # build + run
```

Exécution locale :

```sh
export PLEX_URL=http://localhost:32400
export PLEX_TOKEN=…           # X-Plex-Token
export FEED_TOKEN=test
./plex-rss
# puis :
curl localhost:8080/healthz
curl 'localhost:8080/feeds?token=test'
curl -X POST localhost:8081/refresh/all
curl 'localhost:8080/feed/1?token=test'
```

## Configuration Sonarr / Radarr

Dans **Settings → Connect → Webhook**, déclencheurs « On Import » (+ « On
Upgrade »), URL :

```
http://plex-rss.mediacenter.svc.cluster.local:8081/refresh/<sectionKey>
```

(Sonarr → section Séries, Radarr → section Films ; les `sectionKey` se lisent via
`GET /feeds`.)

## Docker

```sh
docker build -t plex-rss .
docker run --rm -p 8080:8080 -p 8081:8081 \
  -e PLEX_URL=http://plex:32400 -e PLEX_TOKEN=… -e FEED_TOKEN=… \
  -v "$(pwd)/cache:/cache" \
  plex-rss
```

L'image publiée est `ghcr.io/rclsilver/plex-rss`.
