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
// whether it was allowed (migrations 001 and 004). Entries are only ever added: the database
// refuses updates and deletes on this table, and the API has no route that changes one.
type AuditLog struct {
	ID uuid.UUID `json:"id" gorm:"type:uuid;primary_key"`
	// Who. UserID is empty when nobody was logged in (a failed login, a request without a
	// token). It is not a foreign key: an entry keeps the ID after the user is deleted.
	UserID    *uuid.UUID `json:"user_id,omitempty" gorm:"type:uuid;index:idx_audit_user"`
	ActorRole *string    `json:"actor_role,omitempty" gorm:"type:varchar(20)"`
	// What: the permission used (authz), or "METHOD /route" for a route without one
	Action       string  `json:"action" gorm:"type:varchar(100);not null;index:idx_audit_action"`
	ResourceType *string `json:"resource_type,omitempty" gorm:"type:varchar(50);index:idx_audit_resource,priority:1"`
	ResourceID   *string `json:"resource_id,omitempty" gorm:"type:varchar(255);index:idx_audit_resource,priority:2"`
	// success, denied (401 or 403) or failure (any other 4xx or 5xx)
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
	// When
	CreatedAt time.Time `json:"created_at" gorm:"autoCreateTime;index:idx_audit_time"`
}

// TableName is the table from migration 001.
func (AuditLog) TableName() string { return "audit_logs" }

// BeforeCreate assigns a new ID if none is set.
func (a *AuditLog) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}
