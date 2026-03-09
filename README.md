<p align="center">
  <img src="ui/static/favicon.svg" width="80" height="80" alt="Pebble logo" />
</p>

<h1 align="center">Pebble</h1>

<p align="center">
  A lightweight, self-hosted Caddyfile manager with a modern web UI and GitHub sync.
</p>

<p align="center">
  <img src="https://img.shields.io/badge/Go-00ADD8?logo=go&logoColor=white" alt="Go" />
  <img src="https://img.shields.io/badge/Svelte_5-FF3E00?logo=svelte&logoColor=white" alt="Svelte" />
  <img src="https://img.shields.io/badge/Caddy-00A7E1?logo=caddy&logoColor=white" alt="Caddy" />
  <img src="https://img.shields.io/badge/Docker-2496ED?logo=docker&logoColor=white" alt="Docker" />
</p>

---

Pebble is a homelab-focused tool for managing [Caddy](https://caddyserver.com) reverse proxy configurations. It bundles a Go backend, the Caddy binary, and a Svelte 5 single-page app into a single container — no external databases, no separate config repos needed out of the box.

## Features

- **In-browser Caddyfile editor** — CodeMirror-based with custom Caddyfile syntax highlighting, dark/light themes, and keyboard shortcuts (`⌘S` / `Ctrl+S` to save & reload).
- **File tree sidebar** — Create, rename, move (drag & drop), and delete files and folders. Organize configs with `import` directives.
- **Instant Caddy reload** — Every save automatically reloads Caddy. Errors are surfaced in the UI.
- **GitHub sync** — Bidirectional sync via the GitHub API. Auto-pulls remote changes and pushes local edits every 60 seconds.
- **Cloudflare Tunnel support** — Optional variant with built-in `cloudflared` for exposing services without port forwarding.
- **Distroless container** — Minimal attack surface, no shell, no package manager in the final image.
- **Single binary** — The UI is embedded into the Go binary at build time. No separate static file hosting required.

## Quick Start

### Docker Compose (recommended)

```yaml
services:
  pebble:
    image: ghcr.io/lucawahlen/pebble:latest
    ports:
      - "80:80"
      - "443:443"
      - "3000:3000"
    volumes:
      - pebble-config:/etc/pebble
      - caddy-files:/etc/caddy
    restart: unless-stopped

volumes:
  pebble-config:
  caddy-files:
```

```sh
docker compose up -d
```

Open **http://localhost:3000** to access the editor.

Port **80/443** are for Caddy to serve your actual sites. Port **3000** is the Pebble management UI.

### Docker Run

```sh
docker run -d \
  --name pebble \
  -p 80:80 -p 443:443 -p 3000:3000 \
  -v pebble-config:/etc/pebble \
  -v caddy-files:/etc/caddy \
  --restart unless-stopped \
  ghcr.io/lucawahlen/pebble:latest
```

## Images

Two separate packages are published from a single multi-target Dockerfile:

| Image | Description                                                                                 |
|---|---------------------------------------------------------------------------------------------|
| `ghcr.io/lucawahlen/pebble` | Base image — Pebble + Caddy                                                                 |
| `ghcr.io/lucawahlen/pebble-tunnel` | Adds Cloudflare Tunnel (`cloudflared`) |

Both share the same version tags. Use whichever fits your setup.

### Building locally

```sh
# Base image
docker build --target pebble -t pebble .

# Tunnel variant
docker build --target pebble-tunnel -t pebble-tunnel .
```

## Configuration

All configuration is done through environment variables. Settings can also be changed at runtime through the UI's settings panel.

### Environment Variables

| Variable | Default | Description |
|---|---|---|
| `PEBBLE_PASSWORD` | — | Password for the management UI. If unset, a setup screen prompts on first visit |
| `DISABLE_AUTH` | `false` | Set to `true` to completely disable authentication (use only in trusted networks) |
| `PORT` | `3000` | Port for the Pebble management UI |
| `HOST` | `0.0.0.0` | Bind address |
| `CADDYFILES_DIR` | `/etc/caddy` | Directory for Caddy config files |
| `PEBBLE_CONFIG` | `/etc/pebble/config.json` | Path to the Pebble settings file |
| `GITHUB_TOKEN` | — | GitHub Personal Access Token |
| `GITHUB_REPO` | — | GitHub repository (`owner/repo`) |
| `GITHUB_BRANCH` | `main` | Branch to sync with |
| `SYNC_ENABLED` | `true` | Enable/disable automatic GitHub sync |
| `TUNNEL_TOKEN` | — | Cloudflare Tunnel token *(tunnel image only)* |

### Volumes

| Path | Purpose |
|---|---|
| `/etc/caddy` | Caddy configuration files (your Caddyfiles) |
| `/etc/pebble` | Pebble settings (GitHub config, persisted across restarts) |

## Authentication

Pebble requires a password to access the management UI and all API endpoints.

### Setting a password

There are two ways to configure the password:

**Option 1: Environment variable** — Set `PEBBLE_PASSWORD` before starting the container:

```yaml
environment:
  - PEBBLE_PASSWORD=your-secure-password
```

**Option 2: UI setup** — If no `PEBBLE_PASSWORD` is set, the first visit to the UI presents a setup screen where you create a password (minimum 8 characters). The password is persisted to the config file and survives container restarts.

### How it works

- All `/api/*` endpoints (except `/api/auth/*`) require a valid session.
- Sessions are stored in `HttpOnly`, `SameSite=Strict` cookies that expire after 7 days.
- If any API call returns `401`, the UI automatically shows the login screen.
- A logout button appears in the header bar when auth is active.
- The `/health` endpoint and the `/welcome` page remain public.

### API endpoints

| Endpoint | Method | Description |
|---|---|---|
| `/api/auth/check` | `GET` | Returns `{authenticated, authRequired, needsSetup}` |
| `/api/auth/setup` | `POST` | Set initial password (`{"password": "..."}`) — only works once |
| `/api/auth/login` | `POST` | Login (`{"password": "..."}`) — returns session cookie |
| `/api/auth/logout` | `POST` | Invalidates session and clears cookie |

## GitHub Sync

Connect a GitHub repository to version-control your Caddy configuration and sync it across instances.

### Setup

1. Create a [GitHub Personal Access Token](https://github.com/settings/tokens):
   - **Classic token**: needs `repo` scope
   - **Fine-grained token**: needs `Contents: Read and Write` permission
2. Click the **GitHub icon** in the Pebble header bar.
3. Enter your token, repository (`owner/repo`), and branch.
4. Click **Test** to verify the connection, then **Save**.

### How it works

- **Auto-sync** — When enabled, Pebble polls GitHub every 60 seconds. If the remote branch has new commits, it pulls the latest files and reloads Caddy. If local files changed, it pushes them.
- **Pull** — Replaces all local config files with the contents of the repository.
- **Push** — Commits all local config files to the repository.
- **Startup sync** — On container start, Pebble pulls the latest config from GitHub if sync is enabled.

You can also trigger pull/push manually from the settings panel.

### Environment variables vs UI config

Environment variables fill in empty fields on first load only. Once you save settings through the UI, those values are persisted and take precedence. `SYNC_ENABLED` only applies when no config file exists yet. `PEBBLE_PASSWORD` always takes priority over a UI-set password when both are present.

## Cloudflare Tunnel

The `pebble-tunnel` image includes `cloudflared` for exposing services without opening ports on your router.

```yaml
services:
  pebble:
    image: ghcr.io/lucawahlen/pebble-tunnel:latest
    ports:
      - "3000:3000"
      - "80:80"
      - "443:443"
    environment:
      - TUNNEL_TOKEN=your-tunnel-token-here
    volumes:
      - pebble-config:/etc/pebble
      - caddy-files:/etc/caddy
    restart: unless-stopped

volumes:
  pebble-config:
  caddy-files:
```

Get your tunnel token from the [Cloudflare Zero Trust dashboard](https://one.dash.cloudflare.com/) under **Networks → Tunnels**.

## Health Check

Pebble exposes a `GET /health` endpoint that returns `{"status":"ok"}`. The Docker images include a built-in `HEALTHCHECK` that uses this endpoint.

## Architecture

```
┌────────────────────────────────────────────────┐
│  Docker Container                              │
│                                                │
│  ┌──────────────┐    ┌─────────────────────┐   │
│  │ Pebble Server│    │       Caddy         │   │
│  │  (Go + SPA)  │◄──►│  (reverse proxy)    │   │
│  │  :3000       │    │  :80 / :443         │   │
│  └──────┬───────┘    └──────────┬──────────┘   │
│         │                       │              │
│         ▼                       ▼              │
│  /etc/caddy/          Your configured sites    │
│  (Caddyfiles)                                  │
│                                                │
│  ┌──────────────────┐  ┌───────────────────┐   │
│  │  GitHub Sync     │  │  cloudflared      │   │
│  │  (poll every 60s)│  │  (tunnel image)   │   │
│  └──────────────────┘  └───────────────────┘   │
└────────────────────────────────────────────────┘
```

**Tech stack:**
- **Backend** — Go 1.25, stdlib `net/http`, embedded static assets
- **Frontend** — Svelte 5, SvelteKit (static adapter), Tailwind CSS 4, CodeMirror 6
- **Reverse proxy** — Caddy v2, managed as a child process
- **Container** — Multi-stage build, `distroless/static-debian12` base

## Development

### Prerequisites

- Go 1.25+
- Node.js 22+
- Caddy (optional, for local testing with reload)

### UI

```sh
cd ui
npm install
npm run dev
```

The dev server runs on `http://localhost:5173` with HMR.

### Server

```sh
cd server
# Copy the UI build into the expected embed directory
mkdir -p static && cp -r ../ui/build/* static/
go run .
```

The server starts on `http://localhost:3000`.
