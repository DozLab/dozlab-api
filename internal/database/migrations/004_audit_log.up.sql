-- The audit log: who did what to which record, from where, when, and whether it was allowed
-- (docs/decision.md, "Enterprise readiness"). The table exists since 001 but nothing wrote to it.

-- What the request was and how it ended
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS actor_role VARCHAR(20);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS outcome VARCHAR(20) NOT NULL DEFAULT 'success'
    CONSTRAINT audit_logs_outcome_check CHECK (outcome IN ('attempted', 'success', 'denied', 'failure'));
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS method VARCHAR(10);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS path VARCHAR(255);
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS status_code INTEGER;
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS metadata JSONB;
-- Ties a change's 'attempted' entry, written before it runs, to the entry with its outcome
ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS request_id UUID;

CREATE INDEX IF NOT EXISTS idx_audit_resource ON audit_logs(resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_audit_outcome ON audit_logs(outcome);
CREATE INDEX IF NOT EXISTS idx_audit_request ON audit_logs(request_id);

-- An entry keeps the user's ID after the user is deleted. The foreign key would set it to NULL,
-- which is an update, and updates are refused below.
ALTER TABLE audit_logs DROP CONSTRAINT IF EXISTS audit_logs_user_id_fkey;

-- Entries are only ever added. Updates, deletes and TRUNCATE fail for every database user,
-- including the one the API connects as.
-- The one exception is the retention purge (internal/audit, AUDIT_RETENTION_DAYS): a DELETE in
-- a transaction that has set dozlab.audit_purge to 'on' may remove entries older than 30 days.
-- Nothing younger than 30 days can be deleted, whatever the API is configured with.
CREATE OR REPLACE FUNCTION audit_logs_append_only() RETURNS trigger AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF current_setting('dozlab.audit_purge', true) = 'on' THEN
            IF OLD.created_at < NOW() - INTERVAL '30 days' THEN
                RETURN OLD;
            END IF;
            RAISE EXCEPTION 'audit_logs: entries younger than 30 days cannot be purged';
        END IF;
    END IF;
    RAISE EXCEPTION 'audit_logs is append-only: % is not allowed', TG_OP;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS audit_logs_no_change ON audit_logs;
CREATE TRIGGER audit_logs_no_change
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_append_only();

DROP TRIGGER IF EXISTS audit_logs_no_truncate ON audit_logs;
CREATE TRIGGER audit_logs_no_truncate
    BEFORE TRUNCATE ON audit_logs
    FOR EACH STATEMENT EXECUTE FUNCTION audit_logs_append_only();
