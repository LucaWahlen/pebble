package main

import (
	"context"
	"embed"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/LucaWahlen/pebble/server/internal/api"
	"github.com/LucaWahlen/pebble/server/internal/auth"
	"github.com/LucaWahlen/pebble/server/internal/caddy"
	"github.com/LucaWahlen/pebble/server/internal/config"
	"github.com/LucaWahlen/pebble/server/internal/files"
	"github.com/LucaWahlen/pebble/server/internal/github"
)

//go:embed all:static
var embeddedStatic embed.FS

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	// Support health check mode for Docker HEALTHCHECK (distroless has no curl/wget)
	if len(os.Args) > 1 && os.Args[1] == "-health-check" {
		port := envOr("PORT", "3000")
		resp, err := http.Get("http://localhost:" + port + "/health")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		os.Exit(0)
	}

	// ── Resolve all env config once ──
	host := envOr("HOST", "0.0.0.0")
	port := envOr("PORT", "3000")
	caddyfilesDir := envOr("CADDYFILES_DIR", "/etc/caddy")
	configPath := envOr("PEBBLE_CONFIG", "/etc/pebble/config.json")

	syncEnabledEnv := os.Getenv("SYNC_ENABLED")
	var syncOverride *bool
	if syncEnabledEnv != "" {
		v := strings.ToLower(syncEnabledEnv) == "true" || syncEnabledEnv == "1"
		syncOverride = &v
	}

	// ── Build dependencies ──
	configStore := config.NewStore(configPath, config.EnvOverrides{
		GitHubToken:  os.Getenv("GITHUB_TOKEN"),
		GitHubRepo:   os.Getenv("GITHUB_REPO"),
		GitHubBranch: os.Getenv("GITHUB_BRANCH"),
		SyncEnabled:  syncOverride,
	})

	filesSvc := files.NewService(caddyfilesDir)
	caddyMgr := caddy.NewManager(caddyfilesDir)
	ghClient := github.NewClient(&http.Client{Timeout: 30 * time.Second}, filesSvc)
	syncer := github.NewSyncer(ghClient, configStore, caddyMgr)

	disableAuthEnv := strings.ToLower(os.Getenv("DISABLE_AUTH"))
	disableAuth := disableAuthEnv == "true" || disableAuthEnv == "1"
	guard := auth.NewGuard(os.Getenv("PEBBLE_PASSWORD"), configStore, disableAuth)

	if guard.Disabled() {
		log.Println("⚠ Authentication is DISABLED (DISABLE_AUTH=true)")
	} else if guard.Enabled() {
		log.Println("Authentication enabled (PEBBLE_PASSWORD is set)")
	} else {
		log.Println("No PEBBLE_PASSWORD set – initial setup required via UI")
	}

	handler := api.NewHandler(filesSvc, caddyMgr, configStore, ghClient, syncer, guard)

	// ── Bootstrap ──
	caddyMgr.EnsureDefaultCaddyfile(port)
	caddyMgr.Start()

	// Start Cloudflare Tunnel if configured
	tunnelToken := os.Getenv("TUNNEL_TOKEN")
	var tunnelCmd *exec.Cmd
	if tunnelToken != "" {
		log.Println("Starting Cloudflare Tunnel...")
		tunnelCmd = exec.Command("cloudflared", "tunnel", "--no-autoupdate", "run", "--token", tunnelToken)
		tunnelCmd.Stdout = os.Stdout
		tunnelCmd.Stderr = os.Stderr
		if err := tunnelCmd.Start(); err != nil {
			log.Printf("Failed to start cloudflared: %v", err)
			tunnelCmd = nil
		} else {
			log.Printf("Cloudflare Tunnel started (PID %d)", tunnelCmd.Process.Pid)
			go func() {
				if err := tunnelCmd.Wait(); err != nil {
					log.Printf("cloudflared exited: %v", err)
				}
			}()
		}
	}

	// Initial GitHub sync if configured
	conf := configStore.Get()
	if conf.SyncEnabled && conf.GitHubToken != "" && conf.GitHubRepo != "" {
		log.Printf("GitHub sync enabled for %s@%s – pulling and starting poll...", conf.GitHubRepo, conf.GitHubBranch)
		if pulledFiles, sha, err := ghClient.Pull(conf); err != nil {
			log.Printf("Initial pull failed: %v", err)
		} else {
			log.Printf("Initial pull: %d files synced", len(pulledFiles))
			syncer.SetLastKnownSHA(sha)
		}
		syncer.StartPolling()
	}

	// ── HTTP server ──
	staticSubFS, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		log.Fatal("failed to create sub filesystem:", err)
	}
	fileServer := http.FileServer(http.FS(staticSubFS))

	mux := handler.Routes()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Serve static files with SPA fallback
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		// Serve static file if it exists
		if _, err := fs.Stat(staticSubFS, path); err == nil {
			fileServer.ServeHTTP(w, r)
			return
		}
		// Try path.html (e.g. /welcome -> welcome.html)
		if data, err := fs.ReadFile(staticSubFS, path+".html"); err == nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(data)
			return
		}
		// SPA fallback
		data, err := fs.ReadFile(staticSubFS, "index.html")
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(data)
	})

	addr := host + ":" + port
	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// ── Graceful shutdown ──
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		sig := <-sigCh
		log.Printf("Received %s, shutting down...", sig)
		syncer.StopPolling()
		if tunnelCmd != nil && tunnelCmd.Process != nil {
			_ = tunnelCmd.Process.Signal(syscall.SIGTERM)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		caddyMgr.Stop()
		guard.Stop()
	}()

	log.Printf("Server listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
