// Package config handles Pebble settings persistence and env overlay.
// The GitHub token is encrypted at rest using AES-256-GCM.
package config

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/LucaWahlen/pebble/server/internal/encrypt"
)

// Config holds Pebble settings (in-memory, token is plaintext).
type Config struct {
	GitHubToken    string `json:"-"`
	GitHubRepo     string `json:"githubRepo"`
	GitHubBranch   string `json:"githubBranch"`
	GitHubUsername string `json:"githubUsername"`
	SyncEnabled    bool   `json:"syncEnabled"`
}

// diskConfig is the on-disk representation where the token is encrypted.
type diskConfig struct {
	GitHubToken    string `json:"githubToken"`
	GitHubRepo     string `json:"githubRepo"`
	GitHubBranch   string `json:"githubBranch"`
	GitHubUsername string `json:"githubUsername"`
	SyncEnabled    bool   `json:"syncEnabled"`
	PasswordHash   string `json:"passwordHash,omitempty"`
	HMACKey        string `json:"hmacKey,omitempty"`
}

// EnvOverrides holds values from environment variables, resolved once at startup.
type EnvOverrides struct {
	GitHubToken  string
	GitHubRepo   string
	GitHubBranch string
	SyncEnabled  *bool // nil means "not set"
}

// Store manages config load/save with thread safety and env overlay.
type Store struct {
	path         string
	envOverrides EnvOverrides
	encKey       []byte
	mu           sync.RWMutex
	cached       *Config
}

// NewStore creates a config store. configPath is the on-disk JSON path.
// An AES-256 encryption key is auto-generated next to the config file.
func NewStore(configPath string, env EnvOverrides) *Store {
	keyPath := encrypt.DeriveKeyPath(configPath)
	key, err := encrypt.EnsureKey(keyPath)
	if err != nil {
		log.Printf("Warning: failed to initialise encryption key (%v) – token will be stored in plaintext", err)
	}
	return &Store{
		path:         configPath,
		envOverrides: env,
		encKey:       key,
	}
}

// Get returns the current config, loading from disk on first call.
func (s *Store) Get() Config {
	s.mu.RLock()
	if s.cached != nil {
		c := *s.cached
		s.mu.RUnlock()
		return c
	}
	s.mu.RUnlock()

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.cached != nil {
		return *s.cached
	}

	conf := Config{GitHubBranch: "main", SyncEnabled: true}
	loadedFromDisk := false
	data, err := os.ReadFile(s.path)
	if err == nil {
		var dc diskConfig
		if err := json.Unmarshal(data, &dc); err == nil {
			loadedFromDisk = true
			conf.GitHubRepo = dc.GitHubRepo
			conf.GitHubBranch = dc.GitHubBranch
			conf.GitHubUsername = dc.GitHubUsername
			conf.SyncEnabled = dc.SyncEnabled

			// Decrypt token
			if dc.GitHubToken != "" && s.encKey != nil {
				plain, decErr := encrypt.Decrypt(dc.GitHubToken, s.encKey)
				if decErr != nil {
					log.Printf("Warning: failed to decrypt token: %v", decErr)
				} else {
					conf.GitHubToken = plain
				}
			}
		}
	}

	// Overlay env vars (fill in blanks only)
	if s.envOverrides.GitHubToken != "" && conf.GitHubToken == "" {
		conf.GitHubToken = s.envOverrides.GitHubToken
	}
	if s.envOverrides.GitHubRepo != "" && conf.GitHubRepo == "" {
		conf.GitHubRepo = s.envOverrides.GitHubRepo
	}
	if s.envOverrides.GitHubBranch != "" && conf.GitHubBranch == "" {
		conf.GitHubBranch = s.envOverrides.GitHubBranch
	}
	if s.envOverrides.SyncEnabled != nil && !loadedFromDisk {
		conf.SyncEnabled = *s.envOverrides.SyncEnabled
	}

	if conf.GitHubBranch == "" {
		conf.GitHubBranch = "main"
	}
	s.cached = &conf
	return conf
}

// Save persists config to disk (encrypting the token) and updates the cache.
func (s *Store) Save(conf Config) error {
	if conf.GitHubBranch == "" {
		conf.GitHubBranch = "main"
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read existing disk config to preserve password fields
	dc := diskConfig{
		GitHubRepo:     conf.GitHubRepo,
		GitHubBranch:   conf.GitHubBranch,
		GitHubUsername: conf.GitHubUsername,
		SyncEnabled:    conf.SyncEnabled,
	}
	existingData, readErr := os.ReadFile(s.path)
	if readErr == nil {
		var existing diskConfig
		if err := json.Unmarshal(existingData, &existing); err == nil {
			dc.PasswordHash = existing.PasswordHash
			dc.HMACKey = existing.HMACKey
		}
	}

	// Encrypt token before writing to disk
	if conf.GitHubToken != "" && s.encKey != nil {
		enc, err := encrypt.Encrypt(conf.GitHubToken, s.encKey)
		if err != nil {
			log.Printf("Warning: failed to encrypt token: %v – storing in plaintext", err)
			dc.GitHubToken = conf.GitHubToken
		} else {
			dc.GitHubToken = enc
		}
	} else {
		dc.GitHubToken = conf.GitHubToken
	}

	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(dc, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.path, data, 0600); err != nil {
		return err
	}
	s.cached = &conf
	return nil
}

// SavePasswordHash persists the password hash and HMAC key to the config file.
func (s *Store) SavePasswordHash(passwordHash, hmacKey string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Read existing disk config
	dc := diskConfig{GitHubBranch: "main", SyncEnabled: true}
	data, err := os.ReadFile(s.path)
	if err == nil {
		_ = json.Unmarshal(data, &dc)
	}

	dc.PasswordHash = passwordHash
	dc.HMACKey = hmacKey

	if err := os.MkdirAll(filepath.Dir(s.path), 0755); err != nil {
		return err
	}
	out, err := json.MarshalIndent(dc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, out, 0600)
}

// LoadPasswordHash reads the persisted password hash and HMAC key from disk.
// Returns ("", "", nil) if no password is stored.
func (s *Store) LoadPasswordHash() (passwordHash, hmacKey string, err error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return "", "", nil // file doesn't exist yet
	}
	var dc diskConfig
	if err := json.Unmarshal(data, &dc); err != nil {
		return "", "", nil
	}
	return dc.PasswordHash, dc.HMACKey, nil
}

