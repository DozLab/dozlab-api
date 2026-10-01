package audit

import (
	"os"
	"strings"
	"testing"
	"time"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"github.com/google/uuid"
)

// Runs only against a real audit store: a PostgreSQL database with
// internal/database/audit_migrations applied, and three logins.
//
//	AUDIT_TEST_WRITER_URL  a member of dozlab_audit_writer (what the API writes with)
//	AUDIT_TEST_READER_URL  a member of dozlab_audit_reader (what the admin endpoint reads with)
//	AUDIT_TEST_OWNER_URL   the database's owner (never used by the API)
//
// The store's rules are grants, triggers and a function, so SQLite can't test them.
func TestPostgres_AuditStore(t *testing.T) {
	writerURL, readerURL, ownerURL := os.Getenv("AUDIT_TEST_WRITER_URL"), os.Getenv("AUDIT_TEST_READER_URL"), os.Getenv("AUDIT_TEST_OWNER_URL")
	if writerURL == "" || readerURL == "" || ownerURL == "" {
		t.Skip("AUDIT_TEST_WRITER_URL, AUDIT_TEST_READER_URL and AUDIT_TEST_OWNER_URL not set")
	}
	open := func(url string) *database.Database {
		db, err := database.Initialize(url)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	writer, reader, owner := open(writerURL), open(readerURL), open(ownerURL)

	refused := func(t *testing.T, db *database.Database, what, sql string, args ...interface{}) {
		t.Helper()
		if err := db.DB.Exec(sql, args...).Error; err == nil {
			t.Errorf("%s succeeded; it must be refused", what)
		}
	}

	// An entry, written the way the middleware writes it, with the writer login
	userID, requestID := uuid.New(), uuid.New()
	username, role, resourceType, resourceID, ip, agent := "admin1", "admin", "users", uuid.NewString(), "203.0.113.7", "audit-test/1.0"
	oldValues, newValues := models.JSONText(`{"role":"student"}`), models.JSONText(`{"role":"instructor"}`)
	entry := &models.AuditLog{
		RequestID: &requestID, UserID: &userID, ActorUsername: &username, ActorRole: &role,
		Action: "users:update_role", ResourceType: &resourceType, ResourceID: &resourceID,
		Outcome: OutcomeSuccess, Method: "PUT", Path: "/api/v1/admin/users/" + resourceID + "/role", StatusCode: 200,
		OldValues: &oldValues, NewValues: &newValues, IPAddress: &ip, UserAgent: &agent,
	}
	if err := NewDBRecorder(writer).Record(entry); err != nil {
		t.Fatalf("the writer could not add an entry: %v", err)
	}
	attempt := &models.AuditLog{Action: "POST /api/v1/labs/", Outcome: OutcomeAttempted, RequestID: &requestID}
	if err := NewDBRecorder(writer).Record(attempt); err != nil {
		t.Errorf("an attempted entry was refused: %v", err)
	}
	if err := NewDBRecorder(writer).Record(&models.AuditLog{Action: "x", Outcome: "maybe"}); err == nil {
		t.Error("an entry with an unknown outcome was accepted")
	}

	t.Run("the writer can only add", func(t *testing.T) {
		var n int64
		if err := writer.DB.Raw("SELECT count(*) FROM audit_logs").Scan(&n).Error; err == nil {
			t.Error("the writer can read the audit log")
		}
		refused(t, writer, "UPDATE as the writer", "UPDATE audit_logs SET outcome = 'denied'")
		refused(t, writer, "DELETE as the writer", "DELETE FROM audit_logs")
		refused(t, writer, "TRUNCATE as the writer", "TRUNCATE audit_logs")
		refused(t, writer, "DROP TABLE as the writer", "DROP TABLE audit_logs")
		refused(t, writer, "disabling the trigger as the writer", "ALTER TABLE audit_logs DISABLE TRIGGER audit_logs_no_change")
		refused(t, writer, "changing the retention as the writer", "UPDATE audit_settings SET retention_days = 0")
		// It can't choose when the store says it received an entry
		refused(t, writer, "setting received_at as the writer",
			"INSERT INTO audit_logs (id, action, outcome, created_at, received_at) VALUES (?, 'x', 'success', NOW(), '2000-01-01')", uuid.New())
		// A second copy of an entry is refused by its ID, which the spool counts as delivered
		copy := *entry
		err := NewDBRecorder(writer).Record(&copy)
		if err == nil || !isDuplicate(err) {
			t.Errorf("a second copy of an entry: err = %v, want a duplicate-key error", err)
		}
	})

	t.Run("the reader can only read", func(t *testing.T) {
		var got models.AuditLog
		if err := reader.DB.First(&got, "id = ?", entry.ID).Error; err != nil {
			t.Fatalf("the reader could not read the entry: %v", err)
		}
		if got.Action != "users:update_role" || got.UserID == nil || *got.UserID != userID || *got.ActorUsername != "admin1" {
			t.Errorf("entry read back wrong: %+v", got)
		}
		if got.IPAddress == nil || !strings.HasPrefix(*got.IPAddress, "203.0.113.7") {
			t.Errorf("ip_address = %v", got.IPAddress)
		}
		if got.ReceivedAt == nil || time.Since(*got.ReceivedAt) > time.Minute {
			t.Errorf("received_at = %v, want the store's own time", got.ReceivedAt)
		}
		var newRole string
		if err := reader.DB.Raw("SELECT new_values->>'role' FROM audit_logs WHERE id = ?", entry.ID).Scan(&newRole).Error; err != nil || newRole != "instructor" {
			t.Errorf("new_values isn't queryable JSON: role=%q err=%v", newRole, err)
		}
		if err := NewDBRecorder(reader).Record(&models.AuditLog{Action: "x", Outcome: OutcomeSuccess}); err == nil {
			t.Error("the reader can add entries")
		}
		refused(t, reader, "UPDATE as the reader", "UPDATE audit_logs SET outcome = 'denied'")
		refused(t, reader, "DELETE as the reader", "DELETE FROM audit_logs")
		if _, err := Purge(reader); err == nil {
			t.Error("the reader can run the purge")
		}
	})

	t.Run("not even the owner can change or remove an entry", func(t *testing.T) {
		refused(t, owner, "UPDATE as the owner", "UPDATE audit_logs SET outcome = 'denied' WHERE id = ?", entry.ID)
		refused(t, owner, "DELETE as the owner", "DELETE FROM audit_logs WHERE id = ?", entry.ID)
		refused(t, owner, "TRUNCATE as the owner", "TRUNCATE audit_logs")
		refused(t, owner, "a retention under 30 days", "UPDATE audit_settings SET retention_days = 7")
	})

	t.Run("retention is the store's, and the purge keeps to it", func(t *testing.T) {
		var days int
		if err := reader.DB.Raw("SELECT retention_days FROM audit_settings").Scan(&days).Error; err != nil || days != 30 {
			t.Fatalf("retention_days = %d (err %v), want the default 30", days, err)
		}
		old := &models.AuditLog{Action: "old-" + uuid.NewString()[:8], Outcome: OutcomeSuccess, CreatedAt: time.Now().AddDate(0, 0, -45)}
		if err := NewDBRecorder(writer).Record(old); err != nil {
			t.Fatalf("record old entry: %v", err)
		}
		removed, err := Purge(writer)
		if err != nil {
			t.Fatalf("the writer could not run the purge: %v", err)
		}
		if removed != 1 {
			t.Errorf("purge removed %d entries, want 1 (the 45-day-old one)", removed)
		}
		count := func(where string, args ...interface{}) int64 {
			var n int64
			reader.DB.Model(&models.AuditLog{}).Where(where, args...).Count(&n)
			return n
		}
		if count("id = ?", old.ID) != 0 {
			t.Error("the 45-day-old entry is still there after the purge")
		}
		if count("id = ?", entry.ID) != 1 {
			t.Error("the purge removed an entry younger than 30 days")
		}
		if count("action = ?", "audit:purge") < 1 {
			t.Error("the purge isn't on record")
		}
		// Nothing left to remove
		if removed, _ := Purge(writer); removed != 0 {
			t.Errorf("a second purge removed %d entries", removed)
		}
		// Even the owner, pretending to be the purge, can't delete a young entry
		tx := owner.DB.Begin()
		tx.Exec("SELECT set_config('dozlab.audit_purge', 'on', true)")
		if err := tx.Exec("DELETE FROM audit_logs WHERE id = ?", entry.ID).Error; err == nil {
			t.Error("an entry younger than 30 days was deleted with the purge setting on")
		}
		tx.Rollback()

		// Retention 0 keeps everything
		if err := owner.DB.Exec("UPDATE audit_settings SET retention_days = 0").Error; err != nil {
			t.Fatalf("set retention 0: %v", err)
		}
		older := &models.AuditLog{Action: "older-" + uuid.NewString()[:8], Outcome: OutcomeSuccess, CreatedAt: time.Now().AddDate(-2, 0, 0)}
		if err := NewDBRecorder(writer).Record(older); err != nil {
			t.Fatal(err)
		}
		if removed, _ := Purge(writer); removed != 0 {
			t.Errorf("with retention 0 the purge removed %d entries", removed)
		}
		owner.DB.Exec("UPDATE audit_settings SET retention_days = 30")
	})
}
