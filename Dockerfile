# --- UI build ---
FROM node:22-alpine AS ui-builder
WORKDIR /app
COPY ui/package*.json ./
RUN npm ci --ignore-scripts
COPY ui/ ./
RUN npm run build

# --- Go build (Pebble + Caddy) ---
FROM golang:1.25-alpine AS go-builder
WORKDIR /app
RUN CGO_ENABLED=0 GOOS=linux go install -ldflags="-s -w" -trimpath github.com/caddyserver/caddy/v2/cmd/caddy@latest
COPY server/go.mod ./
RUN go mod download
COPY server/ ./
COPY --from=ui-builder /app/build ./static
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -trimpath -o pebble-server .

# --- Download cloudflared (only pulled when building the tunnel target) ---
FROM alpine:3.20 AS cf-downloader
ARG TARGETARCH
RUN apk add --no-cache curl && \
    curl -fsSL -o /usr/local/bin/cloudflared \
      https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${TARGETARCH} && \
    chmod +x /usr/local/bin/cloudflared

# ============================================================
# Target: pebble (default)
#   docker build --target pebble -t ghcr.io/lucawahlen/pebble .
# ============================================================
FROM gcr.io/distroless/static-debian12 AS pebble

COPY --from=go-builder /go/bin/caddy /usr/local/bin/caddy
COPY --from=go-builder /app/pebble-server /usr/local/bin/pebble-server

ENV PORT=3000 \
    HOST=0.0.0.0 \
    CADDYFILES_DIR=/etc/caddy \
    PEBBLE_CONFIG=/etc/pebble/config.json \
    GITHUB_REPO="" \
    GITHUB_BRANCH="main" \
    SYNC_ENABLED="true"

VOLUME ["/etc/pebble", "/etc/caddy"]

EXPOSE 3000 80 443

HEALTHCHECK --interval=30s --timeout=5s --start-period=5s --retries=3 \
  CMD ["/usr/local/bin/pebble-server", "-health-check"]

ENTRYPOINT ["/usr/local/bin/pebble-server"]

# ============================================================
# Target: pebble-tunnel
#   docker build --target pebble-tunnel -t ghcr.io/lucawahlen/pebble-tunnel .
# ============================================================
FROM pebble AS pebble-tunnel

COPY --from=cf-downloader /usr/local/bin/cloudflared /usr/local/bin/cloudflared


