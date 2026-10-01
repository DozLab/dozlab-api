-- The audit store: a database of its own, apart from the app's (docs/decision.md, "Enterprise
-- readiness"). Apply this to the audit database as its owner, never to the app database.
--
-- Three kinds of access, so that whoever can change app data can't change the record of it:
--   the owner          applies these migrations and sets the retention; not used by the API
--   dozlab_audit_writer  may add entries and run the purge; can't read, change or remove one
--   dozlab_audit_reader  may read entries; can't add, change or remove one
-- The two roles can't log in. Give each a login of its own:
--   CREATE ROLE dozlab_api_audit   LOGIN PASSWORD '...' IN ROLE dozlab_audit_writer;
--   CREATE ROLE dozlab_audit_admin LOGIN PASSWORD '...' IN ROLE dozlab_audit_reader;

DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'dozlab_audit_writer') THEN
        CREATE ROLE dozlab_audit_writer NOLOGIN;
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'dozlab_audit_reader') THEN
        CREATE ROLE dozlab_audit_reader NOLOGIN;
    END IF;
END
$$;

-- Who did what, to which record, from where, when, and how it ended. Nothing here refers to
-- the app's tables: an entry carries what it needs and stays readable after the user or the
-- record is gone.
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY,
    -- Ties a change's 'attempted' entry, written before it runs, to the entry with its outcome
    request_id UUID,
    user_id UUID,
    actor_username VARCHAR(50),
    actor_role VARCHAR(20),
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(50),
    resource_id VARCHAR(255),
    outcome VARCHAR(20) NOT NULL
        CONSTRAINT audit_logs_outcome_check CHECK (outcome IN ('attempted', 'success', 'denied', 'failure')),
    method VARCHAR(10),
    path VARCHAR(255),
    status_code INTEGER,
    old_values JSONB,
    new_values JSONB,
    metadata JSONB,
    ip_address INET,
    user_agent TEXT,
    -- When it happened, as the API reports it
    created_at TIMESTAMP WITH TIME ZONE NOT NULL,
    -- When this store received it. The writer can't set it, so a late or backdated entry shows.
    received_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_user ON audit_logs(user_id);
CREATE INDEX idx_audit_action ON audit_logs(action);
CREATE INDEX idx_audit_time ON audit_logs(created_at);
CREATE INDEX idx_audit_resource ON audit_logs(resource_type, resource_id);
CREATE INDEX idx_audit_outcome ON audit_logs(outcome);
CREATE INDEX idx_audit_request ON audit_logs(request_id);

-- How long entries are kept. It lives here, not in the API's configuration, so the API can't
-- shorten it. 0 keeps entries for ever; otherwise at least 30 days. The owner changes it:
--   UPDATE audit_settings SET retention_days = 365;
CREATE TABLE audit_settings (
    only_row BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (only_row),
    retention_days INTEGER NOT NULL DEFAULT 30 CHECK (retention_days = 0 OR retention_days >= 30)
);
INSERT INTO audit_settings DEFAULT VALUES;

-- Entries are only ever added. UPDATE, DELETE and TRUNCATE fail for every user, the owner
-- included, except the DELETE that audit_purge() below does.
CREATE FUNCTION audit_logs_append_only() RETURNS trigger AS $$
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

CREATE TRIGGER audit_logs_no_change
    BEFORE UPDATE OR DELETE ON audit_logs
    FOR EACH ROW EXECUTE FUNCTION audit_logs_append_only();

CREATE TRIGGER audit_logs_no_truncate
    BEFORE TRUNCATE ON audit_logs
    FOR EACH STATEMENT EXECUTE FUNCTION audit_logs_append_only();

-- Removes the entries older than the retention, records that it did, and returns how many.
-- It runs with the owner's rights, so the writer can call it without being able to delete.
CREATE FUNCTION audit_purge() RETURNS BIGINT AS $$
DECLARE
    days INTEGER;
    removed BIGINT;
BEGIN
    SELECT retention_days INTO days FROM audit_settings;
    IF days IS NULL OR days = 0 THEN
        RETURN 0;
    END IF;
    PERFORM set_config('dozlab.audit_purge', 'on', true);
    DELETE FROM audit_logs WHERE created_at < NOW() - make_interval(days => days);
    GET DIAGNOSTICS removed = ROW_COUNT;
    PERFORM set_config('dozlab.audit_purge', 'off', true);
    IF removed > 0 THEN
        INSERT INTO audit_logs (id, action, outcome, metadata, created_at)
        VALUES (gen_random_uuid(), 'audit:purge', 'success',
                jsonb_build_object('removed', removed, 'retention_days', days), NOW());
    END IF;
    RETURN removed;
END;
$$ LANGUAGE plpgsql SECURITY DEFINER SET search_path = public, pg_temp;

-- Access. Nobody gets anything by default.
REVOKE ALL ON audit_logs, audit_settings FROM PUBLIC;
REVOKE ALL ON FUNCTION audit_purge() FROM PUBLIC;
REVOKE ALL ON FUNCTION audit_logs_append_only() FROM PUBLIC;

-- The writer adds entries: every column except received_at. No SELECT, UPDATE or DELETE.
GRANT INSERT (id, request_id, user_id, actor_username, actor_role, action, resource_type,
              resource_id, outcome, method, path, status_code, old_values, new_values, metadata,
              ip_address, user_agent, created_at)
    ON audit_logs TO dozlab_audit_writer;
GRANT EXECUTE ON FUNCTION audit_purge() TO dozlab_audit_writer;

-- The reader reads entries and the retention setting
GRANT SELECT ON audit_logs, audit_settings TO dozlab_audit_reader;
