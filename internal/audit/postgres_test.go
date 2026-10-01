package audit

import (
	"os"
	"testing"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"github.com/google/uuid"
)

// Runs only against a real PostgreSQL with the migrations applied (001–004):
//
//	AUDIT_TEST_DATABASE_URL=postgres://... go test ./internal/audit/
//
// The append-only rule is a database trigger, so SQLite can't test it.
func TestPostgres_AuditLogIsAppendOnly(t *testing.T) {
	url := os.Getenv("AUDIT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("AUDIT_TEST_DATABASE_URL not set")
	}
	db, err := database.Initialize(url)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer db.Close()

	// A user with an entry, written the way the middleware writes it
	user := models.User{ID: uuid.New(), Username: "audit-" + uuid.NewString()[:8], Email: uuid.NewString()[:8] + "@example.com", PasswordHash: "x", Role: "admin", IsActive: true}
	if err := db.DB.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	role, resourceType, resourceID, ip, agent := "admin", "users", uuid.NewString(), "203.0.113.7", "audit-test/1.0"
	oldValues, newValues := models.JSONText(`{"role":"student"}`), models.JSONText(`{"role":"instructor"}`)
	entry := &models.AuditLog{
		UserID: &user.ID, ActorRole: &role, Action: "users:update_role",
		ResourceType: &resourceType, ResourceID: &resourceID,
		Outcome: OutcomeSuccess, Method: "PUT", Path: "/api/v1/admin/users/" + resourceID + "/role", StatusCode: 200,
		OldValues: &oldValues, NewValues: &newValues, IPAddress: &ip, UserAgent: &agent,
	}
	if err := NewDBRecorder(db).Record(entry); err != nil {
		t.Fatalf("record: %v", err)
	}

	// It reads back as written, including the jsonb and inet columns
	var got models.AuditLog
	if err := db.DB.First(&got, "id = ?", entry.ID).Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Action != "users:update_role" || got.UserID == nil || *got.UserID != user.ID || got.CreatedAt.IsZero() {
		t.Errorf("entry read back wrong: %+v", got)
	}
	if got.IPAddress == nil || (*got.IPAddress != "203.0.113.7" && *got.IPAddress != "203.0.113.7/32") {
		t.Errorf("ip_address = %v", got.IPAddress)
	}
	var newRole string
	if err := db.DB.Raw("SELECT new_values->>'role' FROM audit_logs WHERE id = ?", entry.ID).Scan(&newRole).Error; err != nil || newRole != "instructor" {
		t.Errorf("new_values isn't queryable JSON: role=%q err=%v", newRole, err)
	}

	// The database refuses every way of changing or removing an entry
	for name, sql := range map[string]string{
		"UPDATE":   "UPDATE audit_logs SET outcome = 'denied' WHERE id = '" + entry.ID.String() + "'",
		"DELETE":   "DELETE FROM audit_logs WHERE id = '" + entry.ID.String() + "'",
		"TRUNCATE": "TRUNCATE audit_logs",
	} {
		if err := db.DB.Exec(sql).Error; err == nil {
			t.Errorf("%s on audit_logs succeeded; it must be refused", name)
		}
	}
	// An outcome outside the allowed three is refused too
	bad := &models.AuditLog{Action: "x", Outcome: "maybe"}
	if err := NewDBRecorder(db).Record(bad); err == nil {
		t.Error("an entry with an unknown outcome was accepted")
	}

	// Deleting the user works and the entry keeps the user's ID
	if err := db.DB.Delete(&models.User{}, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("deleting a user with audit entries failed: %v", err)
	}
	var after models.AuditLog
	if err := db.DB.First(&after, "id = ?", entry.ID).Error; err != nil {
		t.Fatalf("entry gone after deleting the user: %v", err)
	}
	if after.UserID == nil || *after.UserID != user.ID || after.Outcome != OutcomeSuccess {
		t.Errorf("entry changed after deleting the user: %+v", after)
	}
}
