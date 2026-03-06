package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

//go:embed all:static
var embeddedStatic embed.FS

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// resolveSafe resolves relativePath under the caddy files directory,
// preventing path traversal.
func resolveSafe(relativePath string) (string, error) {
	base := filepath.Clean(envOr("CADDYFILES_DIR", "/etc/caddy"))
	target := filepath.Clean(filepath.Join(base, relativePath))
	if target != base && !strings.HasPrefix(target, base+string(filepath.Separator)) {
		return "", fmt.Errorf("access denied")
	}
	return target, nil
}

type fileEntry struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	IsDirectory bool    `json:"isDirectory"`
	Size        *int64  `json:"size,omitempty"`
	ModifiedAt  *string `json:"modifiedAt,omitempty"`
}

func listDirRecursive(dir, base string) []fileEntry {
	var entries []fileEntry
	items, err := os.ReadDir(dir)
	if err != nil {
		return entries
	}
	for _, item := range items {
		relativePath := item.Name()
		if base != "" {
			relativePath = base + "/" + item.Name()
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		modTime := info.ModTime().UTC().Format(time.RFC3339)
		entry := fileEntry{
			Name:        item.Name(),
			Path:        relativePath,
			IsDirectory: item.IsDir(),
			ModifiedAt:  &modTime,
		}
		if !item.IsDir() {
			size := info.Size()
			entry.Size = &size
		}
		entries = append(entries, entry)
		if item.IsDir() {
			children := listDirRecursive(filepath.Join(dir, item.Name()), relativePath)
			entries = append(entries, children...)
		}
	}
	return entries
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("writeJSON encode error: %v", err)
	}
}

// handleFilesRoot handles GET /api/files (list all files).
func handleFilesRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	caddyfilesDir := envOr("CADDYFILES_DIR", "/etc/caddy")
	files := listDirRecursive(caddyfilesDir, "")
	if files == nil {
		files = []fileEntry{}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"files": files,
		"root":  caddyfilesDir,
	})
}

// handleFile handles GET/PUT/POST/DELETE/PATCH /api/files/{path}.
func handleFile(w http.ResponseWriter, r *http.Request) {
	relativePath := strings.TrimPrefix(r.URL.Path, "/api/files/")

	switch r.Method {
	case http.MethodGet:
		filePath, err := resolveSafe(relativePath)
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
		writeJSON(w, http.StatusOK, map[string]string{
			"content": string(content),
			"path":    relativePath,
		})

	case http.MethodPut:
		filePath, err := resolveSafe(relativePath)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Access denied"})
			return
		}
		var body struct {
			Content string `json:"content"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid content"})
			return
		}
		if err := os.MkdirAll(filepath.Dir(filePath), 0755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create directory"})
			return
		}
		if err := os.WriteFile(filePath, []byte(body.Content), 0644); err != nil {
			log.Printf("Write error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to write file"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": relativePath})

	case http.MethodPost:
		filePath, err := resolveSafe(relativePath)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Access denied"})
			return
		}
		var body struct {
			Content string `json:"content"`
		}
		// ignore decode error – default to empty string
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
		writeJSON(w, http.StatusCreated, map[string]any{"success": true, "path": relativePath})

	case http.MethodDelete:
		filePath, err := resolveSafe(relativePath)
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
			err = os.RemoveAll(filePath)
		} else {
			err = os.Remove(filePath)
		}
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": relativePath})

	case http.MethodPatch:
		filePath, err := resolveSafe(relativePath)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Access denied"})
			return
		}
		var body struct {
			NewPath string `json:"newPath"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.NewPath) == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid newPath"})
			return
		}
		newRelPath := strings.TrimSpace(body.NewPath)
		newFilePath, err := resolveSafe(newRelPath)
		if err != nil {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "Access denied"})
			return
		}
		if err := os.MkdirAll(filepath.Dir(newFilePath), 0755); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create directory"})
			return
		}
		if err := os.Rename(filePath, newFilePath); err != nil {
			log.Printf("Rename error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to rename file"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "path": newRelPath})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleCaddyReload handles POST /api/caddy/reload.
func handleCaddyReload(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	configFile := filepath.Join(envOr("CADDYFILES_DIR", "/etc/caddy"), "Caddyfile")
	cmd := exec.Command("caddy", "reload", "--config", configFile, "--adapter", "caddyfile")
	output, err := cmd.CombinedOutput()
	if err != nil {
		if isNotFoundErr(err) {
			writeJSON(w, http.StatusOK, map[string]any{
				"success": true,
				"skipped": true,
				"reason":  "caddy binary not found",
			})
			return
		}
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		log.Printf("Caddy reload failed: %s", msg)
		writeJSON(w, http.StatusInternalServerError, map[string]any{
			"success": false,
			"error":   msg,
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func isNotFoundErr(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "executable file not found") ||
		strings.Contains(s, "no such file or directory")
}

func main() {
	staticSubFS, err := fs.Sub(embeddedStatic, "static")
	if err != nil {
		log.Fatal("failed to create sub filesystem:", err)
	}
	fileServer := http.FileServer(http.FS(staticSubFS))

	mux := http.NewServeMux()

	// API routes
	mux.HandleFunc("/api/files", handleFilesRoot)
	mux.HandleFunc("/api/files/", handleFile)
	mux.HandleFunc("/api/caddy/reload", handleCaddyReload)

	// Static files with SPA fallback – unknown paths serve index.html
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		if _, err := fs.Stat(staticSubFS, path); err != nil {
			// Not found in static FS – serve the SPA shell
			data, readErr := fs.ReadFile(staticSubFS, "index.html")
			if readErr != nil {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(data)
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	host := envOr("HOST", "0.0.0.0")
	port := envOr("PORT", "3000")
	addr := host + ":" + port
	log.Printf("Server listening on %s", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatal(err)
	}
}
