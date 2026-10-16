package database

import "embed"

// Migrations are the main database's SQL migrations and AuditMigrations the audit store's,
// built into the binaries so cmd/migrate needs no files next to it. Only the .up.sql files
// are applied; the .down.sql files are for undoing a migration by hand.
var (
	//go:embed migrations/*.up.sql
	Migrations embed.FS
	//go:embed audit_migrations/*.up.sql
	AuditMigrations embed.FS
)
