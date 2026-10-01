package audit

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"time"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"github.com/google/uuid"
)

// Store is the audit store as the API uses it: one connection that can only add entries, and
// optionally a second, read-only one for the admin endpoint. The two use different database
// logins (internal/database/audit_migrations), so the credential the API writes with can't
// read, change or remove an entry.
type Store struct {
	// Writer adds entries: the store itself, behind the spool when one is configured.
	Writer Recorder
	// Reader is the read-only connection; nil when AUDIT_READ_DATABASE_URL isn't set.
	Reader *database.Database

	writerDB *database.Database
	spool    *Spool
}

// Config is the audit store's configuration, from the environment.
type Config struct {
	WriterURL string // AUDIT_DATABASE_URL: a login that may only add entries
	ReaderURL string // AUDIT_READ_DATABASE_URL: a login that may only read them (optional)
	SpoolDir  string // AUDIT_SPOOL_DIR: where entries wait while the store is unreachable (optional)
	Required  bool   // AUDIT_REQUIRED: without it, a missing WriterURL logs entries instead
}

// Open connects to the audit store.
func Open(cfg Config) (*Store, error) {
	s := &Store{}
	if cfg.WriterURL == "" {
		if cfg.Required {
			return nil, errors.New("AUDIT_DATABASE_URL is required (the audit store; set AUDIT_REQUIRED=false to run without one)")
		}
		log.Printf("audit: no AUDIT_DATABASE_URL; entries go to the server log only")
		s.Writer = LogRecorder{}
		return s, nil
	}

	writerDB, err := database.Initialize(cfg.WriterURL)
	if err != nil {
		return nil, errors.New("audit store: " + err.Error())
	}
	s.writerDB = writerDB
	s.Writer = NewDBRecorder(writerDB)

	if cfg.SpoolDir != "" {
		spool, err := NewSpool(s.Writer, cfg.SpoolDir)
		if err != nil {
			writerDB.Close()
			return nil, err
		}
		s.spool, s.Writer = spool, spool
	}

	if cfg.ReaderURL != "" {
		reader, err := database.Initialize(cfg.ReaderURL)
		if err != nil {
			writerDB.Close()
			return nil, errors.New("audit store (read): " + err.Error())
		}
		s.Reader = reader
	} else {
		log.Printf("audit: no AUDIT_READ_DATABASE_URL; GET /api/v1/admin/audit-logs is off")
	}
	return s, nil
}

// Start runs the background work until ctx is done: forwarding spooled entries, and asking the
// store once a day to remove the entries older than its retention.
func (s *Store) Start(ctx context.Context) {
	if s.spool != nil {
		go s.spool.Run(ctx, 5*time.Second)
	}
	if s.writerDB != nil {
		go runRetention(ctx, s.writerDB)
	}
}

// Close closes the connections.
func (s *Store) Close() {
	if s.writerDB != nil {
		s.writerDB.Close()
	}
	if s.Reader != nil {
		s.Reader.Close()
	}
}

// DBRecorder adds entries to the audit_logs table of the database it is given.
type DBRecorder struct {
	db *database.Database
}

// NewDBRecorder returns a Recorder that writes to db.
func NewDBRecorder(db *database.Database) *DBRecorder {
	return &DBRecorder{db: db}
}

// recordTimeout bounds one write, so a store that hangs can't hang the request with it.
const recordTimeout = 3 * time.Second

// Record adds one entry. It only inserts: the store's writer login can do nothing else.
func (r *DBRecorder) Record(entry *models.AuditLog) error {
	prepare(entry)
	ctx, cancel := context.WithTimeout(context.Background(), recordTimeout)
	defer cancel()
	return r.db.DB.WithContext(ctx).Create(entry).Error
}

// LogRecorder writes entries to the server log. For development without an audit store.
type LogRecorder struct{}

// Record logs one entry as JSON.
func (LogRecorder) Record(entry *models.AuditLog) error {
	prepare(entry)
	b, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	log.Printf("AUDIT %s", b)
	return nil
}

// An entry gets its ID and time when it is made, not when the store receives it, so one that
// waited in the spool keeps both.
func prepare(entry *models.AuditLog) {
	if entry.ID == uuid.Nil {
		entry.ID = uuid.New()
	}
	if entry.CreatedAt.IsZero() {
		entry.CreatedAt = time.Now()
	}
}

// The retention is the store's own setting (audit_settings.retention_days, 30 days by
// default); audit_purge() removes what is older and records that it did. The API only asks.
func runRetention(ctx context.Context, db *database.Database) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()
	for {
		if removed, err := Purge(db); err != nil {
			log.Printf("audit: retention purge failed: %v", err)
		} else if removed > 0 {
			log.Printf("audit: the store removed %d entries older than its retention", removed)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Purge asks the store to remove the entries older than its retention and returns how many it
// removed.
func Purge(db *database.Database) (int64, error) {
	var removed int64
	err := db.DB.Raw("SELECT audit_purge()").Scan(&removed).Error
	return removed, err
}
