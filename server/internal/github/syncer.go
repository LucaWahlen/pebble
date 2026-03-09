package github

import (
	"log"
	"sync"
	"time"

	"github.com/LucaWahlen/pebble/server/internal/config"
)

// Reloader can reload a Caddy config.
type Reloader interface {
	Reload() (string, bool)
}

// Syncer handles bidirectional GitHub sync polling.
type Syncer struct {
	client   *Client
	config   *config.Store
	reloader Reloader

	lastKnownSHA string
	shaMu        sync.RWMutex
	snapshot     map[string]string
	snapshotMu   sync.RWMutex
	pollStop     chan struct{}
	pollMu       sync.Mutex
}

// NewSyncer creates a sync poller.
func NewSyncer(client *Client, configStore *config.Store, reloader Reloader) *Syncer {
	return &Syncer{
		client:   client,
		config:   configStore,
		reloader: reloader,
	}
}

// SetLastKnownSHA sets the last known remote HEAD SHA (used after initial pull).
func (s *Syncer) SetLastKnownSHA(sha string) {
	s.shaMu.Lock()
	s.lastKnownSHA = sha
	s.shaMu.Unlock()
}

func (s *Syncer) getLastKnownSHA() string {
	s.shaMu.RLock()
	defer s.shaMu.RUnlock()
	return s.lastKnownSHA
}

// StartPolling starts the 60-second sync loop.
func (s *Syncer) StartPolling() {
	s.StopPolling()

	s.pollMu.Lock()
	s.pollStop = make(chan struct{})
	ch := s.pollStop
	s.pollMu.Unlock()

	s.takeSnapshot()

	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		log.Println("[sync] polling started")
		for {
			select {
			case <-ch:
				log.Println("[sync] polling stopped")
				return
			case <-ticker.C:
				s.tick()
			}
		}
	}()
}

// StopPolling stops the sync loop.
func (s *Syncer) StopPolling() {
	s.pollMu.Lock()
	defer s.pollMu.Unlock()
	if s.pollStop != nil {
		close(s.pollStop)
		s.pollStop = nil
	}
}

func (s *Syncer) tick() {
	conf := s.config.Get()
	if !conf.SyncEnabled || conf.GitHubToken == "" || conf.GitHubRepo == "" {
		log.Println("[sync] sync disabled, stopping poll")
		return
	}

	// Check remote
	sha, err := s.client.GetHeadSHA(conf)
	if err != nil {
		log.Printf("[sync] poll error: %v", err)
		return
	}

	lastSHA := s.getLastKnownSHA()
	if sha != lastSHA {
		log.Printf("[sync] remote changed (%s → %s), pulling...", lastSHA, sha)
		files, newSHA, err := s.client.Pull(conf)
		if err != nil {
			log.Printf("[sync] pull error: %v", err)
			return
		}
		s.SetLastKnownSHA(newSHA)
		log.Printf("[sync] pulled %d files", len(files))
		if msg, ok := s.reloader.Reload(); !ok {
			log.Printf("[sync] caddy reload failed after pull: %s", msg)
		} else {
			log.Println("[sync] caddy reloaded after pull")
		}
		s.takeSnapshot()
		return
	}

	// Check local changes
	if s.hasLocalChanges() {
		log.Println("[sync] local files changed, pushing...")
		newSHA, err := s.client.Push(conf, "auto-sync")
		if err != nil {
			log.Printf("[sync] push error: %v", err)
		} else {
			if newSHA != "" {
				s.SetLastKnownSHA(newSHA)
			}
			log.Println("[sync] pushed local changes")
			s.takeSnapshot()
		}
	}
}

func (s *Syncer) takeSnapshot() {
	snapshot := map[string]string{}
	s.client.Files.WalkFiles(func(relPath string, content []byte) {
		snapshot[relPath] = string(content)
	})
	s.snapshotMu.Lock()
	s.snapshot = snapshot
	s.snapshotMu.Unlock()
}

func (s *Syncer) hasLocalChanges() bool {
	s.snapshotMu.RLock()
	old := s.snapshot
	s.snapshotMu.RUnlock()
	if old == nil {
		return false
	}

	current := map[string]string{}
	s.client.Files.WalkFiles(func(relPath string, content []byte) {
		current[relPath] = string(content)
	})

	if len(current) != len(old) {
		return true
	}
	for k, v := range current {
		if old[k] != v {
			return true
		}
	}
	return false
}

