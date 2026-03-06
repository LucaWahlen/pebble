FROM node:22-alpine AS ui-builder
WORKDIR /app
COPY ui/package*.json ./
RUN npm ci
COPY ui/ ./
RUN npm run build

FROM golang:1.23-alpine AS go-builder
WORKDIR /app
COPY server/go.mod ./
RUN go mod download
COPY server/ ./
COPY --from=ui-builder /app/build ./static
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o pebble-server .

FROM cloudflare/cloudflared:latest AS cloudflared

FROM caddy:2-alpine
WORKDIR /app

COPY --from=go-builder /app/pebble-server /usr/local/bin/pebble-server

COPY --from=cloudflared /usr/local/bin/cloudflared /usr/local/bin/cloudflared

COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh

ENV PORT=3000
ENV HOST=0.0.0.0
ENV CADDYFILES_DIR=/etc/caddy

EXPOSE 3000 80 443

CMD ["/usr/local/bin/docker-entrypoint.sh"]
