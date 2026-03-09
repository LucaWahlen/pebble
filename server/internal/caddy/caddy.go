// Package caddy manages the Caddy web server process lifecycle.
package caddy

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// Manager controls the Caddy process.
type Manager struct {
	caddyfilesDir string
	proc          *exec.Cmd
	mu            sync.Mutex
}

// NewManager creates a Caddy manager for the given config directory.
func NewManager(caddyfilesDir string) *Manager {
	return &Manager{caddyfilesDir: filepath.Clean(caddyfilesDir)}
}

// Start launches Caddy as a background process. No-op if Caddyfile is missing.
func (m *Manager) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	configFile := filepath.Join(m.caddyfilesDir, "Caddyfile")
	if _, err := os.Stat(configFile); err != nil {
		log.Println("No Caddyfile found, skipping Caddy startup.")
		return
	}

	cmd := exec.Command("caddy", "run", "--config", configFile, "--adapter", "caddyfile")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		if isNotFoundErr(err) {
			log.Println("Caddy binary not found, skipping Caddy startup.")
		} else {
			log.Printf("Failed to start Caddy: %v", err)
		}
		return
	}
	m.proc = cmd
	log.Printf("Caddy started (PID %d)", cmd.Process.Pid)

	go func() {
		if err := cmd.Wait(); err != nil {
			log.Printf("Caddy exited: %v", err)
		} else {
			log.Println("Caddy exited cleanly")
		}
		m.mu.Lock()
		if m.proc == cmd {
			m.proc = nil
		}
		m.mu.Unlock()
	}()
}

// Stop sends SIGTERM to Caddy.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proc != nil && m.proc.Process != nil {
		_ = m.proc.Process.Signal(syscall.SIGTERM)
	}
}

// IsRunning checks if Caddy is alive.
func (m *Manager) IsRunning() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.proc == nil || m.proc.Process == nil {
		return false
	}
	return m.proc.Process.Signal(syscall.Signal(0)) == nil
}

// Reload reloads Caddy config, starting Caddy first if not running.
// Returns (message, success).
func (m *Manager) Reload() (string, bool) {
	configFile := filepath.Join(m.caddyfilesDir, "Caddyfile")

	if _, err := os.Stat(configFile); err != nil {
		return "no Caddyfile found", true
	}

	if !m.IsRunning() {
		log.Println("Caddy not running, starting it...")
		m.Start()
		time.Sleep(500 * time.Millisecond)
		if m.IsRunning() {
			return "", true
		}
		return "failed to start Caddy", false
	}

	cmd := exec.Command("caddy", "reload", "--config", configFile, "--adapter", "caddyfile")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if isNotFoundErr(err) {
			return "caddy binary not found", true
		}
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		log.Printf("Caddy reload failed: %s", msg)
		return msg, false
	}
	return "", true
}

// EnsureDefaultCaddyfile creates a starter Caddyfile if the directory is empty.
func (m *Manager) EnsureDefaultCaddyfile(port string) {
	caddyfilePath := filepath.Join(m.caddyfilesDir, "Caddyfile")

	if _, err := os.Stat(caddyfilePath); err == nil {
		return
	}

	entries, err := os.ReadDir(m.caddyfilesDir)
	if err == nil && len(entries) > 0 {
		return
	}

	_ = os.MkdirAll(m.caddyfilesDir, 0755)
	m.writeDefaultCaddyfile(caddyfilePath, port)
	log.Println("Created default Caddyfile with catch-all site")
}

func (m *Manager) writeDefaultCaddyfile(caddyfilePath, port string) {
	defaultConfig := fmt.Sprintf(`# Pebble — Caddy Configuration
#
# This is your main Caddyfile. Edit it here or create
# additional files and import them below.
#
# Docs: https://caddyserver.com/docs/caddyfile

# Catch-all — shows the welcome page for unconfigured domains
:80 {
	@notasset {
		not path /_app/* /favicon.svg /robots.txt /welcome
	}
	rewrite @notasset /welcome
	reverse_proxy localhost:%s
}

# ─── Add your sites below ───
#
# Example: Reverse proxy to a local service
#
# app.example.com {
#     reverse_proxy localhost:8080
# }
#
# Example: Import additional config files
#
# import sites/*
`, port)

	if err := os.WriteFile(caddyfilePath, []byte(defaultConfig), 0644); err != nil {
		log.Printf("Failed to write Caddyfile: %v", err)
	}
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "executable file not found") ||
		strings.Contains(s, "no such file or directory")
}

