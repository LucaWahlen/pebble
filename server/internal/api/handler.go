// Package api provides HTTP handlers for the Pebble API.
package api

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/LucaWahlen/pebble/server/internal/auth"
	"github.com/LucaWahlen/pebble/server/internal/caddy"
	"github.com/LucaWahlen/pebble/server/internal/config"
	"github.com/LucaWahlen/pebble/server/internal/files"
	"github.com/LucaWahlen/pebble/server/internal/github"
)

const maxRequestBodySize = 10 << 20 // 10 MB

// Handler holds all dependencies for the API layer.
type Handler struct {
	Files  *files.Service
	Caddy  *caddy.Manager
	Config *config.Store
	GitHub *github.Client
	Syncer *github.Syncer
	Auth   *auth.Guard
}

// NewHandler creates an API handler with all dependencies wired.
func NewHandler(
	filesSvc *files.Service,
	caddyMgr *caddy.Manager,
	configStore *config.Store,
	ghClient *github.Client,
	syncer *github.Syncer,
	guard *auth.Guard,
) *Handler {
	return &Handler{
		Files:  filesSvc,
		Caddy:  caddyMgr,
		Config: configStore,
		GitHub: ghClient,
		Syncer: syncer,
		Auth:   guard,
	}
}

// Routes registers all API routes on a new ServeMux and returns it.
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	// Auth endpoints — always public
	mux.HandleFunc("/api/auth/login", h.handleLogin)
	mux.HandleFunc("/api/auth/logout", h.handleLogout)
	mux.HandleFunc("/api/auth/check", h.handleAuthCheck)
	mux.HandleFunc("/api/auth/setup", h.handleSetup)

	// Protected API endpoints — wrapped with auth middleware
	protected := http.NewServeMux()
	protected.HandleFunc("/api/files", h.handleFilesRoot)
	protected.HandleFunc("/api/files/", h.handleFile)
	protected.HandleFunc("/api/caddy/reload", h.handleCaddyReload)
	protected.HandleFunc("/api/apply", h.handleApply)
	protected.HandleFunc("/api/config", h.handleConfig)
	protected.HandleFunc("/api/github/", h.handleGitHub)
	mux.Handle("/api/", h.Auth.Middleware(protected))

	return mux
}

// ---------- helpers ----------

// limitBody wraps the request body with a max size reader.
func limitBody(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodySize)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

// ---------- /api/auth ----------

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Auth.NeedsSetup() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No password configured. Use setup first."})
		return
	}
	limitBody(w, r)
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	token, ok := h.Auth.Login(body.Password)
	if !ok {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Invalid password"})
		return
	}
	h.Auth.SetSessionCookie(w, token)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	token := h.Auth.TokenFromRequest(r)
	if token != "" {
		h.Auth.Logout(token)
	}
	h.Auth.ClearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (h *Handler) handleAuthCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.Auth.NeedsSetup() {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false, "authRequired": true, "needsSetup": true})
		return
	}
	token := h.Auth.TokenFromRequest(r)
	valid := h.Auth.ValidToken(token)
	writeJSON(w, http.StatusOK, map[string]any{"authenticated": valid, "authRequired": true, "needsSetup": false})
}

func (h *Handler) handleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !h.Auth.NeedsSetup() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "Password already configured"})
		return
	}
	limitBody(w, r)
	var body struct {
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}
	if len(body.Password) < 8 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Password must be at least 8 characters"})
		return
	}
	token, err := h.Auth.SetPassword(body.Password)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": err.Error()})
		return
	}
	h.Auth.SetSessionCookie(w, token)
	log.Println("Initial password configured via UI")
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---------- /api/files ----------

func (h *Handler) handleFilesRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entries := h.Files.ListAll()
	writeJSON(w, http.StatusOK, map[string]any{
		"files": entries,
		"root":  h.Files.RootDir,
	})
}

func (h *Handler) handleFile(w http.ResponseWriter, r *http.Request) {
	limitBody(w, r)
	relativePath := strings.TrimPrefix(r.URL.Path, "/api/files/")

	switch r.Method {
	case http.MethodGet:
		filePath, err := h.Files.ResolveSafe(relativePath)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Access denied"})
			return
		}
		info, err := os.Stat(filePath)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "File not found"})
			return
		}
		if info.IsDir() {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Path is a directory"})
			return
		}
		content, err := os.ReadFile(filePath)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "File not found"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"content": string(content), "path": relativePath})

	case http.MethodPut:
		filePath, err := h.Files.ResolveSafe(relativePath)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Access denied"})
			return
		}
		var body struct{ Content string `json:"content"` }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid content"})
			return
		}
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create directory"})
			return
		}
		if err := os.WriteFile(filePath, []byte(body.Content), 0644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to write file"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": relativePath})

	case http.MethodPost:
		filePath, err := h.Files.ResolveSafe(relativePath)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Access denied"})
			return
		}
		var body struct{ Content string `json:"content"` }
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, err := os.Stat(filePath); err == nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "File already exists"})
			return
		}
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create directory"})
			return
		}
		if err := os.WriteFile(filePath, []byte(body.Content), 0644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create file"})
			return
		}
		h.Files.RemoveGitkeepIfNeeded(filepath.Dir(filePath))
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "path": relativePath})

	case http.MethodDelete:
		filePath, err := h.Files.ResolveSafe(relativePath)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Access denied"})
			return
		}
		info, err := os.Stat(filePath)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "File not found"})
			return
		}
		parentDir := filepath.Dir(filePath)
		if info.IsDir() {
			err = os.RemoveAll(filePath)
		} else {
			err = os.Remove(filePath)
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete"})
			return
		}
		h.Files.AddGitkeepIfEmpty(parentDir)
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": relativePath})

	case http.MethodPatch:
		filePath, err := h.Files.ResolveSafe(relativePath)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Access denied"})
			return
		}
		var body struct{ NewPath string `json:"newPath"` }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.NewPath) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid newPath"})
			return
		}
		newRelPath := strings.TrimSpace(body.NewPath)
		newFilePath, err := h.Files.ResolveSafe(newRelPath)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Access denied"})
			return
		}
		if err := os.MkdirAll(filepath.Dir(newFilePath), 0755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create directory"})
			return
		}
		if err := os.Rename(filePath, newFilePath); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to rename file"})
			return
		}
		h.Files.AddGitkeepIfEmpty(filepath.Dir(filePath))
		h.Files.RemoveGitkeepIfNeeded(filepath.Dir(newFilePath))
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": newRelPath})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------- /api/caddy/reload ----------

func (h *Handler) handleCaddyReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	msg, ok := h.Caddy.Reload()
	if !ok {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"success": false, "error": msg})
		return
	}
	if msg != "" {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "skipped": true, "reason": msg})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---------- /api/apply ----------

func (h *Handler) handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limitBody(w, r)
	var body struct {
		Operations []struct {
			Type    string `json:"type"`
			Path    string `json:"path"`
			NewPath string `json:"newPath"`
			IsDir   bool   `json:"isDir"`
		} `json:"operations"`
		Files []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
		return
	}

	for _, op := range body.Operations {
		switch op.Type {
		case "create":
			p, err := h.Files.ResolveSafe(op.Path)
			if err != nil {
				continue
			}
			if op.IsDir {
				if err := os.MkdirAll(p, 0755); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create directory " + op.Path})
					return
				}
				entries, _ := os.ReadDir(p)
				if len(entries) == 0 {
					_ = os.WriteFile(filepath.Join(p, ".gitkeep"), []byte(""), 0644)
				}
			} else {
				if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create directory for " + op.Path})
					return
				}
				if err := os.WriteFile(p, []byte(""), 0644); err != nil {
					writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create " + op.Path})
					return
				}
				h.Files.RemoveGitkeepIfNeeded(filepath.Dir(p))
			}
		case "delete":
			p, err := h.Files.ResolveSafe(op.Path)
			if err != nil {
				continue
			}
			parentDir := filepath.Dir(p)
			_ = os.RemoveAll(p)
			h.Files.AddGitkeepIfEmpty(parentDir)
		case "rename", "move":
			if op.NewPath == "" {
				continue
			}
			oldP, err := h.Files.ResolveSafe(op.Path)
			if err != nil {
				continue
			}
			newP, err := h.Files.ResolveSafe(op.NewPath)
			if err != nil {
				continue
			}
			if err := os.MkdirAll(filepath.Dir(newP), 0755); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create directory for " + op.NewPath})
				return
			}
			if err := os.Rename(oldP, newP); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to rename " + op.Path + " to " + op.NewPath})
				return
			}
			h.Files.AddGitkeepIfEmpty(filepath.Dir(oldP))
			h.Files.RemoveGitkeepIfNeeded(filepath.Dir(newP))
		}
	}

	var written []string
	for _, f := range body.Files {
		p, err := h.Files.ResolveSafe(f.Path)
		if err != nil {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create directory for " + f.Path})
			return
		}
		if err := os.WriteFile(p, []byte(f.Content), 0644); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to write " + f.Path})
			return
		}
		written = append(written, f.Path)
	}

	var caddyErr string
	if msg, ok := h.Caddy.Reload(); !ok {
		caddyErr = msg
	}

	result := map[string]any{
		"success": true,
		"files":   written,
		"count":   len(written) + len(body.Operations),
	}
	if caddyErr != "" {
		result["caddyError"] = caddyErr
	}
	writeJSON(w, http.StatusOK, result)
}

// ---------- /api/config ----------

func (h *Handler) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		conf := h.Config.Get()
		writeJSON(w, http.StatusOK, map[string]any{
			"githubRepo":     conf.GitHubRepo,
			"githubBranch":   conf.GitHubBranch,
			"githubUsername": conf.GitHubUsername,
			"syncEnabled":    conf.SyncEnabled,
			"hasToken":       conf.GitHubToken != "",
		})

	case http.MethodPut:
		limitBody(w, r)
		var body struct {
			GitHubToken  *string `json:"githubToken"`
			GitHubRepo   string  `json:"githubRepo"`
			GitHubBranch string  `json:"githubBranch"`
			SyncEnabled  bool    `json:"syncEnabled"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
			return
		}
		conf := h.Config.Get()
		if body.GitHubToken != nil {
			conf.GitHubToken = *body.GitHubToken
		}
		conf.GitHubRepo = body.GitHubRepo
		conf.GitHubBranch = body.GitHubBranch
		conf.SyncEnabled = body.SyncEnabled

		if err := h.Config.Save(conf); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save config"})
			return
		}

		if conf.SyncEnabled && conf.GitHubToken != "" && conf.GitHubRepo != "" {
			h.Syncer.StartPolling()
		} else {
			h.Syncer.StopPolling()
		}

		writeJSON(w, http.StatusOK, map[string]any{"success": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// ---------- /api/github/{action} ----------

func (h *Handler) handleGitHub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	limitBody(w, r)

	action := strings.TrimPrefix(r.URL.Path, "/api/github/")
	conf := h.Config.Get()

	switch action {
	case "test":
		var body struct {
			GitHubToken  string `json:"githubToken"`
			GitHubRepo   string `json:"githubRepo"`
			GitHubBranch string `json:"githubBranch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid JSON"})
			return
		}
		token := body.GitHubToken
		if token == "" {
			token = conf.GitHubToken
		}
		repo := body.GitHubRepo
		if repo == "" {
			repo = conf.GitHubRepo
		}
		branch := body.GitHubBranch
		if branch == "" {
			branch = conf.GitHubBranch
		}
		if token == "" || repo == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Token and repo are required"})
			return
		}
		if err := h.GitHub.TestConnection(token, repo, branch); err != nil {
			writeJSON(w, http.StatusOK, map[string]any{"success": false, "error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})

	case "pull":
		if conf.GitHubToken == "" || conf.GitHubRepo == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "GitHub not configured"})
			return
		}
		pulledFiles, sha, err := h.GitHub.Pull(conf)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Pull failed: " + err.Error()})
			return
		}
		h.Syncer.SetLastKnownSHA(sha)
		h.Caddy.Reload()
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"files":   pulledFiles,
			"count":   len(pulledFiles),
		})

	case "push":
		if conf.GitHubToken == "" || conf.GitHubRepo == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "GitHub not configured"})
			return
		}
		newSHA, err := h.GitHub.Push(conf, "manual sync")
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Push failed: " + err.Error()})
			return
		}
		if newSHA != "" {
			h.Syncer.SetLastKnownSHA(newSHA)
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true})

	default:
		http.NotFound(w, r)
	}
}

