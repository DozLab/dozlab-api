// Package audit records who did what, to which record, from where and when, and how it ended.
//
// Entries go to the audit store: a database apart from the app's, which the API can only add
// to (internal/database/audit_migrations). Request bodies are never recorded, so passwords and
// tokens can't end up in the log; handlers add the details worth keeping with Annotate.
package audit

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"dozlab-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// How a request ended. A change also has an earlier entry, attempted, written before it runs.
const (
	OutcomeAttempted = "attempted"
	OutcomeSuccess   = "success"
	OutcomeDenied    = "denied"  // 401 or 403
	OutcomeFailure   = "failure" // any other 4xx or 5xx
)

// Recorder adds entries to the audit log.
type Recorder interface {
	Record(entry *models.AuditLog) error
}

const contextKey = "audit"

// Details is what a handler or middleware adds to the entry for the current request.
type Details struct {
	// Action replaces the default, the route ("POST /api/v1/labs/"). RequirePermission sets
	// it to the permission it checked.
	Action       string
	ResourceType string
	ResourceID   string
	// UserID and Username name the user when the request has no token: a login or a
	// registration.
	UserID   *uuid.UUID
	Username string
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
	// Required refuses a change when it can't be recorded: an "attempted" entry is written
	// before the change runs, and if that fails the request gets 503 and nothing happens. So no
	// change can take place without a trace in the log (owner decision, 2026-10-01). Without
	// it, a failed write is only logged.
	Required bool
}

// Middleware records an entry for each request once it has been handled. Put it before the
// authentication middleware, so requests refused for a missing or bad token are recorded too.
// With Options.Required, a change that can't be recorded is refused; reads are never refused.
func Middleware(recorder Recorder, opts Options) gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		if method == http.MethodOptions {
			c.Next() // CORS preflight
			return
		}
		isRead := method == http.MethodGet || method == http.MethodHead
		requestID := uuid.New()

		if opts.Required && !isRead {
			attempt := &models.AuditLog{
				Action:     method + " " + routeOf(c),
				Outcome:    OutcomeAttempted,
				RequestID:  &requestID,
				Method:     method,
				Path:       c.Request.URL.Path,
				IPAddress:  optional(c.ClientIP()),
				UserAgent:  optional(c.Request.UserAgent()),
				ResourceID: optional(firstParam(c, "labId", "id")),
				CreatedAt:  time.Now(),
			}
			if err := recorder.Record(attempt); err != nil {
				log.Printf("AUDIT WRITE FAILED, request refused: %v (%s %s)", err, method, c.Request.URL.Path)
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": "The audit log is unavailable, so this change was refused",
				})
				c.Abort()
				return
			}
		}

		c.Next()

		status := c.Writer.Status()
		details := Annotate(c)
		outcome := outcomeFor(status)
		if isRead && outcome == OutcomeSuccess && !details.Always && !opts.Reads {
			return
		}

		entry := &models.AuditLog{
			Action:     details.Action,
			Outcome:    outcome,
			RequestID:  &requestID,
			Method:     method,
			Path:       c.Request.URL.Path, // no query string: it can carry tokens
			StatusCode: status,
			UserID:     details.UserID,
			CreatedAt:  time.Now(),
		}
		if entry.Action == "" {
			entry.Action = method + " " + routeOf(c)
		}
		// The entry carries who it was in full: it must stay readable after the user is gone
		if id, ok := c.Get("user_id"); ok {
			if uid, ok := id.(uuid.UUID); ok {
				entry.UserID = &uid
			}
		}
		entry.ActorUsername = optional(contextString(c, "username"))
		if entry.ActorUsername == nil {
			entry.ActorUsername = optional(details.Username)
		}
		entry.ActorRole = optional(contextString(c, "role"))
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

		// Too late to refuse: the request has been handled. With Required, a change already has
		// its attempted entry.
		if err := recorder.Record(entry); err != nil {
			log.Printf("AUDIT WRITE FAILED: %v (action=%s outcome=%s path=%s request_id=%s)", err, entry.Action, entry.Outcome, entry.Path, requestID)
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

func contextString(c *gin.Context, key string) string {
	if v, ok := c.Get(key); ok {
		if s, ok := v.(string); ok {
			return s
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
