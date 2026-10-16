// Command migrate brings DozLab's databases up to date and sets up the logins the API uses.
// The dozlab Helm chart runs it as a hook before every install and upgrade of the API
// (api.migrations), as the Postgres superuser. Running it again is safe:
//
//   - each migration file is applied once, in name order, in its own transaction, and its
//     name recorded in e2e_migrations (the table scripts/e2e-timing.sh used, so databases it
//     set up carry on where they were);
//   - the API's login (APP_DB_USER, default dozlab_app) gets read and write on the main
//     database's tables and nothing else: no DDL, no superuser;
//   - the audit store (AUDIT_DB_NAME, default dozlab_audit) is created if missing, owned by
//     dozlab_audit_owner, and migrated as that role; the API writes it as dozlab_api_audit
//     and administrators read it as dozlab_audit_admin;
//   - every login's password is set to the value given, so changing a value and re-running
//     rotates it.
//
// The superuser connection comes from the standard PGHOST, PGPORT, PGUSER, PGPASSWORD and
// PGSSLMODE variables. Passwords are never logged.
package main

import (
	"context"
	"fmt"
	"io/fs"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"dozlab-backend/internal/database"
)

// settings are the database names and the logins' passwords, from the environment.
type settings struct {
	DBName, AppUser, AppPassword                               string
	AuditDBName, OwnerPassword, WriterPassword, ReaderPassword string
}

const (
	auditOwner  = "dozlab_audit_owner"
	auditWriter = "dozlab_api_audit"   // member of dozlab_audit_writer (audit migration 001)
	auditReader = "dozlab_audit_admin" // member of dozlab_audit_reader
)

func main() {
	log.SetFlags(0)
	log.SetPrefix("migrate: ")
	s, err := loadSettings()
	if err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := run(ctx, s); err != nil {
		log.Fatal(err)
	}
	log.Print("done")
}

func loadSettings() (settings, error) {
	s := settings{
		DBName:         envOr("DB_NAME", "dozlab"),
		AppUser:        envOr("APP_DB_USER", "dozlab_app"),
		AppPassword:    os.Getenv("APP_DB_PASSWORD"),
		AuditDBName:    envOr("AUDIT_DB_NAME", "dozlab_audit"),
		OwnerPassword:  os.Getenv("AUDIT_OWNER_PASSWORD"),
		WriterPassword: os.Getenv("AUDIT_WRITER_PASSWORD"),
		ReaderPassword: os.Getenv("AUDIT_READER_PASSWORD"),
	}
	var missing []string
	for name, v := range map[string]string{
		"APP_DB_PASSWORD": s.AppPassword, "AUDIT_OWNER_PASSWORD": s.OwnerPassword,
		"AUDIT_WRITER_PASSWORD": s.WriterPassword, "AUDIT_READER_PASSWORD": s.ReaderPassword,
	} {
		if v == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return s, fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	return s, nil
}

func run(ctx context.Context, s settings) error {
	// Main database: migrations, then the API's login and its grants.
	app, err := connect(ctx, s.DBName)
	if err != nil {
		return err
	}
	defer app.Close(ctx)
	if err := applyMigrations(ctx, app, database.Migrations, "migrations"); err != nil {
		return fmt.Errorf("%s: %w", s.DBName, err)
	}
	if err := setLogin(ctx, app, s.AppUser, s.AppPassword, ""); err != nil {
		return err
	}
	for _, stmt := range []string{
		"GRANT CONNECT ON DATABASE %[2]s TO %[1]s",
		"GRANT USAGE ON SCHEMA public TO %[1]s",
		"GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO %[1]s",
		"GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO %[1]s",
		"REVOKE ALL ON e2e_migrations FROM %[1]s",
	} {
		q := fmt.Sprintf(stmt, pgx.Identifier{s.AppUser}.Sanitize(), pgx.Identifier{s.DBName}.Sanitize())
		if _, err := app.Exec(ctx, q); err != nil {
			return fmt.Errorf("grant to %s: %w", s.AppUser, err)
		}
	}
	log.Printf("%s: login %s can read and write the tables", s.DBName, s.AppUser)

	// Audit store: its owner and database (from the postgres database), its migrations as the
	// owner, then the API's and administrators' logins (roles the migrations create).
	admin, err := connect(ctx, "postgres")
	if err != nil {
		return err
	}
	defer admin.Close(ctx)
	if err := setLogin(ctx, admin, auditOwner, s.OwnerPassword, "CREATEROLE"); err != nil {
		return err
	}
	var exists bool
	if err := admin.QueryRow(ctx, "SELECT EXISTS (SELECT FROM pg_database WHERE datname = $1)", s.AuditDBName).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		q := fmt.Sprintf("CREATE DATABASE %s OWNER %s", pgx.Identifier{s.AuditDBName}.Sanitize(), pgx.Identifier{auditOwner}.Sanitize())
		if _, err := admin.Exec(ctx, q); err != nil {
			return fmt.Errorf("create %s: %w", s.AuditDBName, err)
		}
		log.Printf("created database %s", s.AuditDBName)
	}

	audit, err := connect(ctx, s.AuditDBName)
	if err != nil {
		return err
	}
	defer audit.Close(ctx)
	if _, err := audit.Exec(ctx, "SET ROLE "+pgx.Identifier{auditOwner}.Sanitize()); err != nil {
		return err
	}
	if err := applyMigrations(ctx, audit, database.AuditMigrations, "audit_migrations"); err != nil {
		return fmt.Errorf("%s: %w", s.AuditDBName, err)
	}

	if err := setLogin(ctx, admin, auditWriter, s.WriterPassword, "IN ROLE dozlab_audit_writer"); err != nil {
		return err
	}
	if err := setLogin(ctx, admin, auditReader, s.ReaderPassword, "IN ROLE dozlab_audit_reader"); err != nil {
		return err
	}
	log.Printf("%s: logins %s (writes) and %s (reads)", s.AuditDBName, auditWriter, auditReader)
	return nil
}

func connect(ctx context.Context, dbname string) (*pgx.Conn, error) {
	cfg, err := pgx.ParseConfig("") // PGHOST, PGPORT, PGUSER, PGPASSWORD, PGSSLMODE
	if err != nil {
		return nil, err
	}
	cfg.Database = dbname
	conn, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", dbname, err)
	}
	return conn, nil
}

// applyMigrations applies each *.up.sql in dir of files that e2e_migrations doesn't list yet,
// in name order. A file and its e2e_migrations row commit together.
func applyMigrations(ctx context.Context, conn *pgx.Conn, files fs.FS, dir string) error {
	if _, err := conn.Exec(ctx, `SET client_min_messages = warning;
CREATE TABLE IF NOT EXISTS e2e_migrations (name text PRIMARY KEY, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return err
	}
	names, err := fs.Glob(files, dir+"/*.up.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)
	applied := 0
	for _, path := range names {
		name := path[len(dir)+1:]
		var done bool
		if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT FROM e2e_migrations WHERE name = $1)", name).Scan(&done); err != nil {
			return err
		}
		if done {
			continue
		}
		sql, err := fs.ReadFile(files, path)
		if err != nil {
			return err
		}
		err = pgx.BeginFunc(ctx, conn, func(tx pgx.Tx) error {
			// No arguments: sent with the simple protocol, so a file may hold many statements.
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO e2e_migrations (name) VALUES ($1)", name)
			return err
		})
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		log.Printf("applied %s/%s", dir, name)
		applied++
	}
	if applied == 0 {
		log.Printf("%s: up to date (%d files)", dir, len(names))
	}
	return nil
}

// setLogin creates role as a login with the given options if it doesn't exist, and sets its
// password. The password goes in as a quoted literal built by the server (format %L), since
// CREATE/ALTER ROLE can't take a bind parameter.
func setLogin(ctx context.Context, conn *pgx.Conn, role, password, createOptions string) error {
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT EXISTS (SELECT FROM pg_roles WHERE rolname = $1)", role).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		q := fmt.Sprintf("CREATE ROLE %s LOGIN %s", pgx.Identifier{role}.Sanitize(), createOptions)
		if _, err := conn.Exec(ctx, q); err != nil {
			return fmt.Errorf("create role %s: %w", role, err)
		}
		log.Printf("created login %s", role)
	}
	var alter string
	if err := conn.QueryRow(ctx, "SELECT format('ALTER ROLE %I WITH LOGIN PASSWORD %L', $1::text, $2::text)", role, password).Scan(&alter); err != nil {
		return err
	}
	if _, err := conn.Exec(ctx, alter); err != nil {
		return fmt.Errorf("set password of %s", role) // the error could quote the statement
	}
	return nil
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
