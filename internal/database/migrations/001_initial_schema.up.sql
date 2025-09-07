-- Initial schema migration for Dozlab backend
-- This migration creates the BCNF-compliant schema with composite primary keys

-- Enable UUID extension
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Core user management and authentication
CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    username VARCHAR(50) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    first_name VARCHAR(100),
    last_name VARCHAR(100),
    role VARCHAR(20) DEFAULT 'student' CHECK (role IN ('admin', 'instructor', 'student')),
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_login_at TIMESTAMP WITH TIME ZONE
);

-- Lab definitions and metadata
CREATE TABLE labs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    slug VARCHAR(100) UNIQUE NOT NULL,
    description TEXT,
    difficulty_level VARCHAR(20) DEFAULT 'beginner' CHECK (difficulty_level IN ('beginner', 'intermediate', 'advanced')),
    estimated_duration INTEGER, -- minutes
    category VARCHAR(50),
    tags TEXT[], -- Array of tags
    is_published BOOLEAN DEFAULT false,
    created_by UUID REFERENCES users(id) ON DELETE SET NULL,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    version INTEGER DEFAULT 1
);

-- Flexible lab specifications stored as JSONB with composite PK
CREATE TABLE lab_specs (
    id UUID UNIQUE NOT NULL DEFAULT gen_random_uuid(),
    lab_id UUID REFERENCES labs(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    specification JSONB NOT NULL, -- Lab configuration, resources, tasks, etc.
    kubernetes_manifest JSONB, -- K8s resources needed
    validation_rules JSONB, -- Rules for automatic checking
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    PRIMARY KEY (lab_id, version)
);

-- Active and historical lab sessions
CREATE TABLE sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    lab_id UUID REFERENCES labs(id) ON DELETE CASCADE,
    lab_spec_id UUID REFERENCES lab_specs(id) ON DELETE SET NULL,
    status VARCHAR(20) DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'completed', 'failed', 'expired')),
    started_at TIMESTAMP WITH TIME ZONE,
    completed_at TIMESTAMP WITH TIME ZONE,
    expires_at TIMESTAMP WITH TIME ZONE,
    worker_node_id VARCHAR(100), -- Which K8s worker is handling this session
    session_data JSONB, -- Dynamic session state, progress, variables
    resource_usage JSONB, -- CPU, memory, storage tracking
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- User progress tracking across labs with composite PK
CREATE TABLE user_progress (
    id UUID UNIQUE NOT NULL DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    lab_id UUID REFERENCES labs(id) ON DELETE CASCADE,
    session_id UUID REFERENCES sessions(id) ON DELETE SET NULL,
    progress_percentage DECIMAL(5,2) DEFAULT 0.00 CHECK (progress_percentage >= 0 AND progress_percentage <= 100),
    completed_tasks JSONB, -- Array of completed task IDs
    current_task VARCHAR(100),
    score DECIMAL(5,2),
    attempts INTEGER DEFAULT 1,
    first_attempt_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    last_attempt_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE,
    PRIMARY KEY (user_id, lab_id)
);

-- System notifications and alerts
CREATE TABLE notifications (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL, -- 'session_expired', 'lab_completed', 'system_alert', etc.
    title VARCHAR(255) NOT NULL,
    message TEXT,
    metadata JSONB, -- Additional notification data
    is_read BOOLEAN DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    expires_at TIMESTAMP WITH TIME ZONE
);

-- User lab enrollments and access control
CREATE TABLE user_lab_enrollments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    lab_id UUID REFERENCES labs(id) ON DELETE CASCADE,
    enrolled_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    access_expires_at TIMESTAMP WITH TIME ZONE,
    enrollment_type VARCHAR(20) DEFAULT 'self' CHECK (enrollment_type IN ('self', 'instructor', 'admin')),
    is_active BOOLEAN DEFAULT true,
    UNIQUE(user_id, lab_id)
);

-- Session resource usage tracking
CREATE TABLE session_resources (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID REFERENCES sessions(id) ON DELETE CASCADE,
    resource_type VARCHAR(50) NOT NULL, -- 'cpu', 'memory', 'storage', 'network'
    usage_value DECIMAL(12,4) NOT NULL,
    unit VARCHAR(10) NOT NULL, -- 'cores', 'MB', 'GB', 'Mbps'
    recorded_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    metadata JSONB -- Additional resource metrics
);

-- Lab prerequisite relationships
CREATE TABLE lab_dependencies (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    lab_id UUID REFERENCES labs(id) ON DELETE CASCADE,
    prerequisite_lab_id UUID REFERENCES labs(id) ON DELETE CASCADE,
    is_required BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    UNIQUE(lab_id, prerequisite_lab_id),
    CHECK (lab_id != prerequisite_lab_id) -- Prevent self-reference
);

-- Worker node registration and health
CREATE TABLE worker_nodes (
    id VARCHAR(100) PRIMARY KEY, -- Node identifier
    name VARCHAR(255) NOT NULL,
    status VARCHAR(20) DEFAULT 'active' CHECK (status IN ('active', 'inactive', 'maintenance', 'error')),
    capacity JSONB NOT NULL, -- Max CPU, memory, sessions, etc.
    current_load JSONB, -- Current resource usage
    last_heartbeat TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    metadata JSONB -- Additional node information
);

-- Audit log for security and compliance
CREATE TABLE audit_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID REFERENCES users(id) ON DELETE SET NULL,
    action VARCHAR(100) NOT NULL,
    resource_type VARCHAR(50),
    resource_id VARCHAR(255),
    old_values JSONB,
    new_values JSONB,
    ip_address INET,
    user_agent TEXT,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW()
);

-- ====================================================
-- Indexes for Performance
-- ====================================================

-- User indexes
CREATE INDEX idx_users_email ON users(email);
CREATE INDEX idx_users_username ON users(username);
CREATE INDEX idx_users_role ON users(role);

-- Lab indexes
CREATE INDEX idx_labs_slug ON labs(slug);
CREATE INDEX idx_labs_category ON labs(category);
CREATE INDEX idx_labs_difficulty ON labs(difficulty_level);
CREATE INDEX idx_labs_published ON labs(is_published);
CREATE INDEX idx_labs_created_by ON labs(created_by);

-- Session indexes
CREATE INDEX idx_sessions_user_id ON sessions(user_id);
CREATE INDEX idx_sessions_lab_id ON sessions(lab_id);
CREATE INDEX idx_sessions_status ON sessions(status);
CREATE INDEX idx_sessions_worker_node ON sessions(worker_node_id);
CREATE INDEX idx_sessions_expires_at ON sessions(expires_at);

-- Progress indexes (composite PK already provides user_id, lab_id index)
CREATE INDEX idx_progress_completion ON user_progress(completed_at);
CREATE INDEX idx_progress_session ON user_progress(session_id);

-- Notification indexes
CREATE INDEX idx_notifications_user_unread ON notifications(user_id, is_read);
CREATE INDEX idx_notifications_expires ON notifications(expires_at);

-- Resource tracking indexes
CREATE INDEX idx_resources_session ON session_resources(session_id);
CREATE INDEX idx_resources_type_time ON session_resources(resource_type, recorded_at);

-- Worker node indexes
CREATE INDEX idx_workers_status ON worker_nodes(status);
CREATE INDEX idx_workers_heartbeat ON worker_nodes(last_heartbeat);

-- Audit log indexes
CREATE INDEX idx_audit_user ON audit_logs(user_id);
CREATE INDEX idx_audit_action ON audit_logs(action);
CREATE INDEX idx_audit_time ON audit_logs(created_at);