package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"dozlab-backend/internal/models"
)

// Spool keeps the API working while the audit store is unreachable. An entry the store doesn't
// take is written to a file in a local directory and synced to disk, and counts as recorded;
// Run sends the waiting entries on once the store answers again. Only when neither the store
// nor the directory can be written does a change get refused.
//
// The directory must survive a restart (a volume, not a container's own filesystem), and it
// holds entries that haven't reached the store yet: keep it readable by the API alone.
type Spool struct {
	store Recorder
	dir   string

	mu sync.Mutex
	// After a failed write, go straight to the spool until this time instead of making every
	// request wait for the store's timeout.
	skipStoreUntil time.Time
}

// How long writes skip the store after it failed.
const storeRetryAfter = 10 * time.Second

// NewSpool returns a Recorder that writes to store and falls back to files in dir.
func NewSpool(store Recorder, dir string) (*Spool, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("audit spool: %w", err)
	}
	// Fail at start, not at the first outage, if the directory can't be written
	probe := filepath.Join(dir, ".writable")
	if err := os.WriteFile(probe, nil, 0o600); err != nil {
		return nil, fmt.Errorf("audit spool: %s is not writable: %w", dir, err)
	}
	os.Remove(probe)
	return &Spool{store: store, dir: dir}, nil
}

// Record sends the entry to the store, or keeps it in the spool when the store doesn't take it.
func (s *Spool) Record(entry *models.AuditLog) error {
	prepare(entry)

	s.mu.Lock()
	skip := time.Now().Before(s.skipStoreUntil)
	s.mu.Unlock()

	if !skip {
		err := s.store.Record(entry)
		if err == nil {
			return nil
		}
		log.Printf("audit: the store did not take an entry, spooling it: %v", err)
		s.mu.Lock()
		s.skipStoreUntil = time.Now().Add(storeRetryAfter)
		s.mu.Unlock()
	}
	return s.write(entry)
}

// One file per entry, named so that sorting by name is sorting by time. Written to a temporary
// name, synced and renamed, so a crash never leaves half an entry to forward.
func (s *Spool) write(entry *models.AuditLog) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%020d-%s.json", entry.CreatedAt.UnixNano(), entry.ID)
	tmp := filepath.Join(s.dir, name+".tmp")
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("audit spool: %w", err)
	}
	if _, err = f.Write(data); err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp, filepath.Join(s.dir, name))
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("audit spool: %w", err)
	}
	if d, derr := os.Open(s.dir); derr == nil {
		d.Sync() // make the rename durable
		d.Close()
	}
	return nil
}

// Pending returns the spooled entries' file names, oldest first.
func (s *Spool) Pending() []string {
	names, _ := filepath.Glob(filepath.Join(s.dir, "*.json"))
	sort.Strings(names)
	return names
}

// Forward sends the waiting entries to the store, oldest first, and stops at the first one the
// store doesn't take. It returns how many it delivered.
func (s *Spool) Forward() (int, error) {
	delivered := 0
	for _, name := range s.Pending() {
		data, err := os.ReadFile(name)
		if err != nil {
			return delivered, err
		}
		var entry models.AuditLog
		if err := json.Unmarshal(data, &entry); err != nil {
			// Not an entry: set it aside for a person to look at, and go on with the rest
			log.Printf("audit spool: %s is unreadable (%v); kept as %s.bad", name, err, name)
			os.Rename(name, name+".bad")
			continue
		}
		// An entry that was delivered just before a crash is sent again; the store refuses the
		// second copy by its ID, and that counts as delivered.
		if err := s.store.Record(&entry); err != nil && !isDuplicate(err) {
			return delivered, err
		}
		if err := os.Remove(name); err != nil {
			return delivered, err
		}
		delivered++
	}
	if delivered > 0 {
		s.mu.Lock()
		s.skipStoreUntil = time.Time{}
		s.mu.Unlock()
	}
	return delivered, nil
}

// Run forwards the waiting entries every interval until ctx is done.
func (s *Spool) Run(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if len(s.Pending()) > 0 {
			if n, err := s.Forward(); n > 0 {
				log.Printf("audit: delivered %d spooled entries to the store", n)
			} else if err != nil {
				log.Printf("audit: %d entries are waiting in the spool; the store is still unreachable: %v", len(s.Pending()), err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func isDuplicate(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key") || strings.Contains(msg, "UNIQUE constraint failed")
}
