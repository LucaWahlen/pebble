// Package files handles safe filesystem operations for the Caddy config directory.
package files

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Entry represents a file or directory in the config tree.
type Entry struct {
	Name        string  `json:"name"`
	Path        string  `json:"path"`
	IsDirectory bool    `json:"isDirectory"`
	Size        *int64  `json:"size,omitempty"`
	ModifiedAt  *string `json:"modifiedAt,omitempty"`
}

// Service provides safe file operations scoped to a root directory.
type Service struct {
	RootDir string
}

// NewService creates a file service for the given root directory.
func NewService(rootDir string) *Service {
	return &Service{RootDir: filepath.Clean(rootDir)}
}

// ResolveSafe resolves a relative path under the root, preventing traversal.
func (s *Service) ResolveSafe(relativePath string) (string, error) {
	target := filepath.Clean(filepath.Join(s.RootDir, relativePath))
	if target != s.RootDir && !strings.HasPrefix(target, s.RootDir+string(filepath.Separator)) {
		return "", fmt.Errorf("access denied")
	}
	return target, nil
}

// ListAll returns all files and directories recursively, excluding .gitkeep.
func (s *Service) ListAll() []Entry {
	entries := listDirRecursive(s.RootDir, "")
	if entries == nil {
		return []Entry{}
	}
	return entries
}

func listDirRecursive(dir, base string) []Entry {
	var entries []Entry
	items, err := os.ReadDir(dir)
	if err != nil {
		return entries
	}
	for _, item := range items {
		if item.Name() == ".gitkeep" {
			continue
		}
		relPath := item.Name()
		if base != "" {
			relPath = base + "/" + item.Name()
		}
		info, err := item.Info()
		if err != nil {
			continue
		}
		modTime := info.ModTime().UTC().Format(time.RFC3339)
		entry := Entry{
			Name:        item.Name(),
			Path:        relPath,
			IsDirectory: item.IsDir(),
			ModifiedAt:  &modTime,
		}
		if !item.IsDir() {
			size := info.Size()
			entry.Size = &size
		}
		entries = append(entries, entry)
		if item.IsDir() {
			children := listDirRecursive(filepath.Join(dir, item.Name()), relPath)
			entries = append(entries, children...)
		}
	}
	return entries
}

// RemoveGitkeepIfNeeded removes .gitkeep from a directory if it has other files.
func (s *Service) RemoveGitkeepIfNeeded(dir string) {
	gitkeep := filepath.Join(dir, ".gitkeep")
	if _, err := os.Stat(gitkeep); err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name() != ".gitkeep" {
			_ = os.Remove(gitkeep)
			return
		}
	}
}

// AddGitkeepIfEmpty adds .gitkeep to a directory if it's empty (but not the root).
func (s *Service) AddGitkeepIfEmpty(dir string) {
	if dir == s.RootDir {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	if len(entries) == 0 {
		_ = os.WriteFile(filepath.Join(dir, ".gitkeep"), []byte{}, 0644)
	}
}

// CleanDir removes all contents of a directory without removing the directory itself.
func (s *Service) CleanDir() {
	entries, err := os.ReadDir(s.RootDir)
	if err != nil {
		return
	}
	for _, entry := range entries {
		_ = os.RemoveAll(filepath.Join(s.RootDir, entry.Name()))
	}
}

// WalkFiles walks all files (not directories) and calls fn with (relativePath, content).
func (s *Service) WalkFiles(fn func(relPath string, content []byte)) {
	_ = filepath.Walk(s.RootDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		relPath, _ := filepath.Rel(s.RootDir, path)
		relPath = filepath.ToSlash(relPath)
		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		fn(relPath, content)
		return nil
	})
}

