// Package auth provides session-based authentication for the Pebble management API.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	sessionCookie  = "pebble_session"
	sessionMaxAge  = 7 * 24 * time.Hour // 7 days
	cleanupEvery   = 1 * time.Hour
)

// session tracks a single authenticated session.
type session struct {
	expiresAt time.Time
}

// PasswordStore can persist and load the password hash.
type PasswordStore interface {
	SavePasswordHash(passwordHash, hmacKey string) error
	LoadPasswordHash() (passwordHash, hmacKey string, err error)
}

// Guard provides password-based session auth for HTTP handlers.
// If no password is configured, all requests are allowed through
// until a password is set via SetPassword.
type Guard struct {
	passwordHash []byte // HMAC-SHA256 of the password
	hmacKey      []byte
	store        PasswordStore

	mu       sync.RWMutex
	sessions map[string]session

	cleanupOnce sync.Once
	stopCleanup chan struct{}
}

// NewGuard creates an auth guard.
// If envPassword is non-empty, it is used directly.
// Otherwise, the guard attempts to load a persisted password from the store.
func NewGuard(envPassword string, store PasswordStore) *Guard {
	g := &Guard{
		sessions:    make(map[string]session),
		stopCleanup: make(chan struct{}),
		store:       store,
	}

	if envPassword != "" {
		// Env password takes priority
		g.initKey()
		g.passwordHash = g.hashLocked(envPassword)
		g.startCleanup()
		return g
	}

	// Try to load persisted password hash from disk
	if store != nil {
		hashHex, keyHex, err := store.LoadPasswordHash()
		if err == nil && hashHex != "" && keyHex != "" {
			key, errK := hex.DecodeString(keyHex)
			hash, errH := hex.DecodeString(hashHex)
			if errK == nil && errH == nil && len(key) == 32 {
				g.hmacKey = key
				g.passwordHash = hash
				g.startCleanup()
			}
		}
	}

	return g
}

// Enabled returns true if a password has been configured.
func (g *Guard) Enabled() bool {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.passwordHash != nil
}

// NeedsSetup returns true if no password has been set yet
// (neither via env nor via the UI).
func (g *Guard) NeedsSetup() bool {
	return !g.Enabled()
}

// SetPassword sets the password at runtime (used for initial setup via UI).
// Returns a session token so the user is logged in immediately after setup.
func (g *Guard) SetPassword(password string) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if g.passwordHash != nil {
		return "", fmt.Errorf("password already configured")
	}

	g.initKeyLocked()
	g.passwordHash = g.hashLocked(password)
	g.startCleanup()

	// Persist to disk
	if g.store != nil {
		hashHex := hex.EncodeToString(g.passwordHash)
		keyHex := hex.EncodeToString(g.hmacKey)
		if err := g.store.SavePasswordHash(hashHex, keyHex); err != nil {
			log.Printf("Warning: failed to persist password to disk: %v", err)
		}
	}

	// Create a session for the user who just set the password
	token := g.newToken()
	g.sessions[token] = session{expiresAt: time.Now().Add(sessionMaxAge)}
	return token, nil
}

// Stop terminates the background cleanup goroutine.
func (g *Guard) Stop() {
	select {
	case <-g.stopCleanup:
		// already closed
	default:
		close(g.stopCleanup)
	}
}

// Login checks the password and returns a session token on success.
func (g *Guard) Login(password string) (string, bool) {
	g.mu.RLock()
	if g.passwordHash == nil {
		g.mu.RUnlock()
		return "", false
	}
	if !hmac.Equal(g.hashLocked(password), g.passwordHash) {
		g.mu.RUnlock()
		return "", false
	}
	g.mu.RUnlock()

	token := g.newToken()
	g.mu.Lock()
	g.sessions[token] = session{expiresAt: time.Now().Add(sessionMaxAge)}
	g.mu.Unlock()
	return token, true
}

// Logout invalidates a session token.
func (g *Guard) Logout(token string) {
	g.mu.Lock()
	delete(g.sessions, token)
	g.mu.Unlock()
}

// ValidToken returns true if the token is valid and not expired.
func (g *Guard) ValidToken(token string) bool {
	if token == "" {
		return false
	}
	g.mu.RLock()
	s, ok := g.sessions[token]
	g.mu.RUnlock()
	return ok && time.Now().Before(s.expiresAt)
}

// Middleware returns an HTTP middleware that protects handlers behind auth.
// If no password is set yet, requests pass through. Once a password is
// configured (via env or SetPassword), a valid session cookie is required.
func (g *Guard) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !g.Enabled() {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie(sessionCookie)
		if err != nil || !g.ValidToken(cookie.Value) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		next.ServeHTTP(w, r)
	})
}

// SetSessionCookie writes the session cookie to the response.
func (g *Guard) SetSessionCookie(w http.ResponseWriter, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Secure:   false, // Pebble may run on plain HTTP in homelab setups
	})
}

// ClearSessionCookie removes the session cookie.
func (g *Guard) ClearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

// TokenFromRequest extracts the session token from the request cookie.
func (g *Guard) TokenFromRequest(r *http.Request) string {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil {
		return ""
	}
	return cookie.Value
}

// ── internal ──

// initKey generates the HMAC key if not already set (caller must NOT hold mu).
func (g *Guard) initKey() {
	if g.hmacKey != nil {
		return
	}
	g.hmacKey = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, g.hmacKey); err != nil {
		panic("auth: failed to generate HMAC key: " + err.Error())
	}
}

// initKeyLocked is like initKey but assumes mu is already held.
func (g *Guard) initKeyLocked() {
	if g.hmacKey != nil {
		return
	}
	g.hmacKey = make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, g.hmacKey); err != nil {
		panic("auth: failed to generate HMAC key: " + err.Error())
	}
}

// hashLocked computes HMAC-SHA256. Assumes hmacKey is set; caller may hold any lock.
func (g *Guard) hashLocked(value string) []byte {
	mac := hmac.New(sha256.New, g.hmacKey)
	mac.Write([]byte(value))
	return mac.Sum(nil)
}

// startCleanup starts the background session cleanup goroutine (once).
func (g *Guard) startCleanup() {
	g.cleanupOnce.Do(func() {
		go g.cleanupLoop()
	})
}

func (g *Guard) newToken() string {
	b := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		panic("auth: failed to generate token: " + err.Error())
	}
	return hex.EncodeToString(b)
}

func (g *Guard) cleanupLoop() {
	ticker := time.NewTicker(cleanupEvery)
	defer ticker.Stop()
	for {
		select {
		case <-g.stopCleanup:
			return
		case <-ticker.C:
			g.purgeExpired()
		}
	}
}

func (g *Guard) purgeExpired() {
	now := time.Now()
	g.mu.Lock()
	defer g.mu.Unlock()
	for token, s := range g.sessions {
		if now.After(s.expiresAt) {
			delete(g.sessions, token)
		}
	}
}







