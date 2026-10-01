// Package audit records who did what, to which record, from where and when, and whether it
// was allowed. Entries go to the audit_logs table, which the database keeps append-only
// (migrations/004_audit_log.up.sql). Request bodies are never recorded, so passwords and
// tokens can't end up in the log; handlers add the details worth keeping with Annotate.
package audit

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// How a request ended.
const (
	OutcomeSuccess = "success"
	OutcomeDenied  = "denied"  // 401 or 403
	OutcomeFailure = "failure" // any other 4xx or 5xx
)

// Recorder stores audit entries. The API has one implementation, DBRecorder; the interface is
// here so the log can move to a store of its own without touching the callers.
type Recorder interface {
	Record(entry *models.AuditLog) error
}

// DBRecorder writes entries to the audit_logs table.
type DBRecorder struct {
	db *database.Database
}

// NewDBRecorder returns a Recorder that writes to db.
func NewDBRecorder(db *database.Database) *DBRecorder {
	return &DBRecorder{db: db}
}

// Record adds one entry.
func (r *DBRecorder) Record(entry *models.AuditLog) error {
	return r.db.DB.Create(entry).Error
}

const contextKey = "audit"

// Details is what a handler or middleware adds to the entry for the current request.
type Details struct {
	// Action replaces the default, the route ("POST /api/v1/labs/"). RequirePermission sets
	// it to the permission it checked.
	Action       string
	ResourceType string
	ResourceID   string
	// UserID names the user when the request has no token: a login or a registration.
	UserID *uuid.UUID
	// Old and New are the values a change replaced and set (a role, a status).
	Old, New map[string]interface{}
	// Metadata is anything else worth keeping, such as the username a failed login tried.
	Metadata map[string]interface{}
	// Always records the request even when it is a successful read.
	Always bool
}

// Annotate returns the details for this request's entry, to be filled in by the caller.
func Annotate(c *gin.Context) *Details {
	if v, ok := c.Get(contextKey); ok {
		if d, ok := v.(*Details); ok {
			return d
		}
	}
	d := &Details{}
	c.Set(contextKey, d)
	return d
}

// SetResource names the record the request acts on.
func SetResource(c *gin.Context, resourceType, resourceID string) {
	d := Annotate(c)
	d.ResourceType, d.ResourceID = resourceType, resourceID
}

// SetMeta adds one key to the entry's metadata.
func SetMeta(c *gin.Context, key string, value interface{}) {
	d := Annotate(c)
	if d.Metadata == nil {
		d.Metadata = map[string]interface{}{}
	}
	d.Metadata[key] = value
}

// Options says which requests the middleware records.
type Options struct {
	// Reads also records successful reads (GET). Without it the log has every change, every
	// refused or failed request, and the reads marked Always (admin routes, the log itself).
	Reads bool
}

// Middleware records an entry for each request once it has been handled. Put it before the
// authentication middleware, so requests refused for a missing or bad token are recorded too.
// A recorder that fails is logged and doesn't fail the request.
func Middleware(recorder Recorder, opts Options) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()

		method := c.Request.Method
		if method == http.MethodOptions {
			return // CORS preflight
		}
		status := c.Writer.Status()
		details := Annotate(c)
		outcome := outcomeFor(status)
		isRead := method == http.MethodGet || method == http.MethodHead
		if isRead && outcome == OutcomeSuccess && !details.Always && !opts.Reads {
			return
		}

		entry := &models.AuditLog{
			Action:     details.Action,
			Outcome:    outcome,
			Method:     method,
			Path:       c.Request.URL.Path, // no query string: it can carry tokens
			StatusCode: status,
			UserID:     details.UserID,
		}
		if entry.Action == "" {
			entry.Action = method + " " + routeOf(c)
		}
		if id, ok := c.Get("user_id"); ok {
			if uid, ok := id.(uuid.UUID); ok {
				entry.UserID = &uid
			}
		}
		if role, ok := c.Get("role"); ok {
			if s, ok := role.(string); ok && s != "" {
				entry.ActorRole = &s
			}
		}
		resourceType, resourceID := details.ResourceType, details.ResourceID
		if resourceType == "" {
			resourceType = resourceTypeOf(entry.Action)
		}
		if resourceID == "" {
			resourceID = firstParam(c, "labId", "id")
		}
		entry.ResourceType = optional(resourceType)
		entry.ResourceID = optional(resourceID)
		entry.IPAddress = optional(c.ClientIP())
		entry.UserAgent = optional(c.Request.UserAgent())
		entry.OldValues = jsonOf(details.Old)
		entry.NewValues = jsonOf(details.New)
		entry.Metadata = jsonOf(details.Metadata)

		if err := recorder.Record(entry); err != nil {
			log.Printf("AUDIT WRITE FAILED: %v (action=%s outcome=%s path=%s)", err, entry.Action, entry.Outcome, entry.Path)
		}
	}
}

func outcomeFor(status int) string {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return OutcomeDenied
	case status >= 400:
		return OutcomeFailure
	default:
		return OutcomeSuccess
	}
}

// The route pattern ("/api/v1/labs/:labId"), or the path when no route matched.
func routeOf(c *gin.Context) string {
	if route := c.FullPath(); route != "" {
		return route
	}
	return c.Request.URL.Path
}

// "labs:update" acts on "labs"; an action that isn't a permission has no data type.
func resourceTypeOf(action string) string {
	if i := strings.Index(action, ":"); i > 0 && !strings.Contains(action, " ") {
		return action[:i]
	}
	return ""
}

func firstParam(c *gin.Context, names ...string) string {
	for _, name := range names {
		if v := c.Param(name); v != "" {
			return v
		}
	}
	return ""
}

func optional(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func jsonOf(m map[string]interface{}) *models.JSONText {
	if len(m) == 0 {
		return nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil
	}
	j := models.JSONText(b)
	return &j
}
