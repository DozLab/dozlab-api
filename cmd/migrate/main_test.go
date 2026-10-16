package main

import (
	"io/fs"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"dozlab-backend/internal/database"
)

// Every .up.sql in the repo is embedded, so a new migration can't be left out of the image.
func TestEmbeddedMigrationsMatchRepo(t *testing.T) {
	for _, tc := range []struct {
		dir   string
		files fs.FS
	}{
		{"migrations", database.Migrations},
		{"audit_migrations", database.AuditMigrations},
	} {
		onDisk, err := filepath.Glob(filepath.Join("..", "..", "internal", "database", tc.dir, "*.up.sql"))
		if err != nil {
			t.Fatal(err)
		}
		embedded, err := fs.Glob(tc.files, tc.dir+"/*.up.sql")
		if err != nil {
			t.Fatal(err)
		}
		if len(embedded) == 0 || len(embedded) != len(onDisk) {
			t.Errorf("%s: %d embedded, %d on disk", tc.dir, len(embedded), len(onDisk))
		}
		for i := range onDisk {
			onDisk[i] = tc.dir + "/" + filepath.Base(onDisk[i])
		}
		sort.Strings(onDisk)
		if strings.Join(embedded, ",") != strings.Join(onDisk, ",") {
			t.Errorf("%s: embedded %v, on disk %v", tc.dir, embedded, onDisk)
		}
	}
}

func TestLoadSettings(t *testing.T) {
	for _, k := range []string{"DB_NAME", "APP_DB_USER", "AUDIT_DB_NAME"} {
		t.Setenv(k, "")
	}
	t.Setenv("APP_DB_PASSWORD", "a")
	t.Setenv("AUDIT_OWNER_PASSWORD", "")
	t.Setenv("AUDIT_WRITER_PASSWORD", "w")
	t.Setenv("AUDIT_READER_PASSWORD", "")
	if _, err := loadSettings(); err == nil || err.Error() != "missing AUDIT_OWNER_PASSWORD, AUDIT_READER_PASSWORD" {
		t.Errorf("missing passwords: got %v", err)
	}

	t.Setenv("AUDIT_OWNER_PASSWORD", "o")
	t.Setenv("AUDIT_READER_PASSWORD", "r")
	s, err := loadSettings()
	if err != nil {
		t.Fatal(err)
	}
	if s.DBName != "dozlab" || s.AppUser != "dozlab_app" || s.AuditDBName != "dozlab_audit" {
		t.Errorf("defaults: %+v", s)
	}
}
