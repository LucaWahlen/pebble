#!/bin/sh
set -e

caddy run --config /etc/caddy/Caddyfile --adapter caddyfile --watch &
CADDY_PID=$!

sleep 1
if ! kill -0 "$CADDY_PID" 2>/dev/null; then
  echo "Caddy failed to start" >&2
  exit 1
fi

if [ -n "$TUNNEL_TOKEN" ]; then
  echo "Starting Cloudflare Tunnel..."
  cloudflared tunnel --no-autoupdate run --token "$TUNNEL_TOKEN" &
  CLOUDFLARED_PID=$!
  sleep 1
  if ! kill -0 "$CLOUDFLARED_PID" 2>/dev/null; then
    echo "cloudflared failed to start" >&2
    exit 1
  fi
fi

exec pebble-server
