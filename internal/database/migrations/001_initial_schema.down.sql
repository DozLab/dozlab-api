-- Down migration for initial schema
-- This will drop all tables in reverse dependency order

DROP TABLE IF EXISTS audit_logs;
DROP TABLE IF EXISTS worker_nodes;
DROP TABLE IF EXISTS lab_dependencies;
DROP TABLE IF EXISTS session_resources;
DROP TABLE IF EXISTS user_lab_enrollments;
DROP TABLE IF EXISTS notifications;
DROP TABLE IF EXISTS user_progress;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS lab_specs;
DROP TABLE IF EXISTS labs;
DROP TABLE IF EXISTS users;

-- Drop extension if no other tables need it
DROP EXTENSION IF EXISTS "uuid-ossp";