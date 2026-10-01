DROP TRIGGER IF EXISTS audit_logs_no_truncate ON audit_logs;
DROP TRIGGER IF EXISTS audit_logs_no_change ON audit_logs;
DROP FUNCTION IF EXISTS audit_logs_append_only();

-- NOT VALID: entries may name users that no longer exist
ALTER TABLE audit_logs ADD CONSTRAINT audit_logs_user_id_fkey
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE SET NULL NOT VALID;

DROP INDEX IF EXISTS idx_audit_request;
DROP INDEX IF EXISTS idx_audit_outcome;
DROP INDEX IF EXISTS idx_audit_resource;

ALTER TABLE audit_logs DROP COLUMN IF EXISTS request_id;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS metadata;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS status_code;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS path;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS method;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS outcome;
ALTER TABLE audit_logs DROP COLUMN IF EXISTS actor_role;
