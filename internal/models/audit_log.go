package models

import (
	"database/sql/driver"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// JSONText is a JSON document stored in a jsonb column and returned as JSON, not as a string.
type JSONText string

// Value sends the document to the database as text, which PostgreSQL casts to jsonb.
func (j JSONText) Value() (driver.Value, error) { return string(j), nil }

// Scan reads the document back; drivers return jsonb as a string or as bytes.
func (j *JSONText) Scan(src interface{}) error {
	switch v := src.(type) {
	case nil:
		*j = ""
	case string:
		*j = JSONText(v)
	case []byte:
		*j = JSONText(v)
	default:
		return fmt.Errorf("JSONText: cannot scan %T", src)
	}
	return nil
}

// MarshalJSON writes the stored document as it is.
func (j JSONText) MarshalJSON() ([]byte, error) {
	if j == "" {
		return []byte("null"), nil
	}
	return []byte(j), nil
}

// UnmarshalJSON keeps the document as it was sent.
func (j *JSONText) UnmarshalJSON(data []byte) error {
	if string(data) == "null" {
		*j = ""
		return nil
	}
	*j = JSONText(data)
	return nil
}

// AuditLog is one entry in the audit log: who did what to which record, from where, when, and
// how it ended. Entries live in the audit store, a database apart from the app's
// (internal/database/audit_migrations), and refer to nothing in the app's tables: an entry
// carries what it needs and stays readable after the user or the record is gone. They are only
// ever added; the store refuses updates and deletes, and removes entries itself once they are
// older than its retention.
type AuditLog struct {
	ID uuid.UUID `json:"id" gorm:"type:uuid;primary_key"`
	// RequestID is the same on a change's attempted entry and on the entry with its outcome
	RequestID *uuid.UUID `json:"request_id,omitempty" gorm:"type:uuid;index:idx_audit_request"`
	// Who. UserID is empty when nobody was logged in (a failed login, a request without a token).
	UserID        *uuid.UUID `json:"user_id,omitempty" gorm:"type:uuid;index:idx_audit_user"`
	ActorUsername *string    `json:"actor_username,omitempty" gorm:"type:varchar(50)"`
	ActorRole     *string    `json:"actor_role,omitempty" gorm:"type:varchar(20)"`
	// What: the permission used (authz), or "METHOD /route" for a route without one
	Action       string  `json:"action" gorm:"type:varchar(100);not null;index:idx_audit_action"`
	ResourceType *string `json:"resource_type,omitempty" gorm:"type:varchar(50);index:idx_audit_resource,priority:1"`
	ResourceID   *string `json:"resource_id,omitempty" gorm:"type:varchar(255);index:idx_audit_resource,priority:2"`
	// attempted (written before a change runs), then success, denied (401 or 403) or failure
	// (any other 4xx or 5xx)
	Outcome    string    `json:"outcome" gorm:"type:varchar(20);not null;index:idx_audit_outcome"`
	Method     string    `json:"method" gorm:"type:varchar(10)"`
	Path       string    `json:"path" gorm:"type:varchar(255)"`
	StatusCode int       `json:"status_code"`
	OldValues  *JSONText `json:"old_values,omitempty" gorm:"type:jsonb"`
	NewValues  *JSONText `json:"new_values,omitempty" gorm:"type:jsonb"`
	Metadata   *JSONText `json:"metadata,omitempty" gorm:"type:jsonb"`
	// From where
	IPAddress *string `json:"ip_address,omitempty" gorm:"type:inet"`
	UserAgent *string `json:"user_agent,omitempty" gorm:"type:text"`
	// When it happened, as the API reports it
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;index:idx_audit_time"`
	// When the store received it. Read-only: the store sets it and the API's writer can't.
	ReceivedAt *time.Time `json:"received_at,omitempty" gorm:"->"`
}

// TableName is the table in the audit store.
func (AuditLog) TableName() string { return "audit_logs" }

// BeforeCreate assigns a new ID if none is set.
func (a *AuditLog) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}
