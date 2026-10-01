package audit

import (
	"testing"
	"time"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestRetentionDays(t *testing.T) {
	tests := []struct {
		value string
		want  int
	}{
		{"", 30},       // not set: the default
		{"30", 30},     // the default, said out loud
		{"365", 365},   // longer is fine
		{"0", 0},       // keep for ever
		{"7", 30},      // below the minimum: raised
		{"29", 30},     // below the minimum: raised
		{"-5", 30},     // nonsense: the default
		{"a year", 30}, // nonsense: the default
	}
	for _, tt := range tests {
		if got := RetentionDays(tt.value); got != tt.want {
			t.Errorf("RetentionDays(%q) = %d, want %d", tt.value, got, tt.want)
		}
	}
}

func testDB(t *testing.T) *database.Database {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("sql.DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := db.AutoMigrate(&models.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &database.Database{DB: db, SqlDB: sqlDB}
}

func TestPurge_RemovesOnlyEntriesOlderThanTheRetention(t *testing.T) {
	db := testDB(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	ages := map[string]int{"today": 0, "29 days": 29, "31 days": 31, "90 days": 90}
	for name, days := range ages {
		entry := &models.AuditLog{Action: name, Outcome: OutcomeSuccess, CreatedAt: now.AddDate(0, 0, -days)}
		if err := db.DB.Create(entry).Error; err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
	}

	removed, err := Purge(db, 30, now)
	if err != nil {
		t.Fatalf("purge: %v", err)
	}
	if removed != 2 {
		t.Errorf("removed %d entries, want 2 (31 and 90 days old)", removed)
	}

	left := map[string]models.AuditLog{}
	var all []models.AuditLog
	if err := db.DB.Find(&all).Error; err != nil {
		t.Fatal(err)
	}
	for _, e := range all {
		left[e.Action] = e
	}
	if len(left) != 3 {
		t.Fatalf("%d entries left, want 3: %v", len(left), left)
	}
	for _, action := range []string{"today", "29 days", "audit:purge"} {
		if _, ok := left[action]; !ok {
			t.Errorf("entry %q is missing after the purge", action)
		}
	}
	// The purge is on record, with how many it removed
	purge := left["audit:purge"]
	if purge.Metadata == nil || !containsAll(string(*purge.Metadata), `"removed":2`, `"retention_days":30`) {
		t.Errorf("purge entry metadata = %v", purge.Metadata)
	}

	// Nothing more to remove: no second purge entry
	if removed, _ := Purge(db, 30, now); removed != 0 {
		t.Errorf("second purge removed %d", removed)
	}
	var total int64
	db.DB.Model(&models.AuditLog{}).Count(&total)
	if total != 3 {
		t.Errorf("%d entries after a second purge, want 3", total)
	}
}

func TestPurge_NeverBelowTheMinimumAndOffAtZero(t *testing.T) {
	db := testDB(t)
	now := time.Now()
	if err := db.DB.Create(&models.AuditLog{Action: "10 days", Outcome: OutcomeSuccess, CreatedAt: now.AddDate(0, 0, -10)}).Error; err != nil {
		t.Fatal(err)
	}
	if removed, _ := Purge(db, 5, now); removed != 0 {
		t.Errorf("a 5-day retention removed a 10-day-old entry; the minimum is %d days", MinRetentionDays)
	}
	if removed, _ := Purge(db, 0, now); removed != 0 {
		t.Errorf("retention 0 (keep for ever) removed %d entries", removed)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		found := false
		for i := 0; i+len(p) <= len(s); i++ {
			if s[i:i+len(p)] == p {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
