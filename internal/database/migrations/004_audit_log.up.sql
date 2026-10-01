-- The audit log moves out of the app's database into a store of its own, with its own
-- credentials (internal/database/audit_migrations; docs/decision.md, "Enterprise readiness").
-- Nothing ever wrote to this table, so there is nothing to carry over.
DROP TABLE IF EXISTS audit_logs;
