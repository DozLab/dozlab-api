DROP FUNCTION IF EXISTS audit_purge();
DROP TABLE IF EXISTS audit_logs;
DROP FUNCTION IF EXISTS audit_logs_append_only();
DROP TABLE IF EXISTS audit_settings;
-- The roles dozlab_audit_writer and dozlab_audit_reader are shared by the whole server and may
-- have logins as members; drop them by hand if they are no longer needed.
