package audit

import (
	"context"
	"log"
	"strconv"
	"time"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"gorm.io/gorm"
)

const (
	// DefaultRetentionDays is how long an entry is kept when AUDIT_RETENTION_DAYS isn't set
	// (owner decision, 2026-10-01).
	DefaultRetentionDays = 30
	// MinRetentionDays is the shortest retention there is. The database enforces it too:
	// migration 004's trigger refuses to delete an entry younger than this.
	MinRetentionDays = 30
)

// RetentionDays reads AUDIT_RETENTION_DAYS: empty or not a number is the default, 0 keeps
// entries for ever, and anything below the minimum is raised to it.
func RetentionDays(value string) int {
	if value == "" {
		return DefaultRetentionDays
	}
	days, err := strconv.Atoi(value)
	if err != nil || days < 0 {
		log.Printf("audit: AUDIT_RETENTION_DAYS=%q is not a number of days; using %d", value, DefaultRetentionDays)
		return DefaultRetentionDays
	}
	if days != 0 && days < MinRetentionDays {
		log.Printf("audit: AUDIT_RETENTION_DAYS=%d is below the minimum; using %d", days, MinRetentionDays)
		return MinRetentionDays
	}
	return days
}

// Purge removes the entries older than days and records that it did, with how many and up to
// when. It returns the number removed. On PostgreSQL the delete runs in a transaction that
// sets dozlab.audit_purge, the one case the append-only trigger lets through.
func Purge(db *database.Database, days int, now time.Time) (int64, error) {
	if days <= 0 {
		return 0, nil
	}
	if days < MinRetentionDays {
		days = MinRetentionDays
	}
	cutoff := now.AddDate(0, 0, -days)

	var removed int64
	err := db.DB.Transaction(func(tx *gorm.DB) error {
		if tx.Dialector.Name() == "postgres" {
			if err := tx.Exec("SELECT set_config('dozlab.audit_purge', 'on', true)").Error; err != nil {
				return err
			}
		}
		result := tx.Where("created_at < ?", cutoff).Delete(&models.AuditLog{})
		removed = result.RowsAffected
		return result.Error
	})
	if err != nil {
		return 0, err
	}
	if removed == 0 {
		return 0, nil
	}

	entry := &models.AuditLog{
		Action:   "audit:purge",
		Outcome:  OutcomeSuccess,
		Metadata: jsonOf(map[string]interface{}{"removed": removed, "older_than": cutoff.UTC().Format(time.RFC3339), "retention_days": days}),
	}
	if err := NewDBRecorder(db).Record(entry); err != nil {
		log.Printf("AUDIT WRITE FAILED: %v (action=audit:purge removed=%d)", err, removed)
	}
	return removed, nil
}

// StartRetention purges once now and then once a day until ctx is done. days <= 0 keeps
// entries for ever.
func StartRetention(ctx context.Context, db *database.Database, days int) {
	if days <= 0 {
		log.Printf("audit: retention is off (AUDIT_RETENTION_DAYS=0); entries are kept for ever")
		return
	}
	log.Printf("audit: entries are kept for %d days", days)
	go func() {
		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for {
			if removed, err := Purge(db, days, time.Now()); err != nil {
				log.Printf("audit: retention purge failed: %v", err)
			} else if removed > 0 {
				log.Printf("audit: removed %d entries older than %d days", removed, days)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}
