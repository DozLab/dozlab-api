package integration

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"dozlab-backend/internal/api"
	"dozlab-backend/internal/audit"
	"dozlab-backend/internal/database"
	"dozlab-backend/internal/models"
	"dozlab-backend/internal/websocket"
	"dozlab-backend/pkg/auth"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

const testSecret = "test-jwt-secret-key-32-characters-long"

// The real router (api.SetupRoutes) on an in-memory app database and a separate in-memory audit
// store, with one user per role.
type accessEnv struct {
	router  *gin.Engine
	db      *database.Database // the app's database: no audit table
	auditDB *database.Database // the audit store
	users   map[string]models.User
	tokens  map[string]string
}

func memoryDB(t *testing.T, tables ...interface{}) *database.Database {
	t.Helper()
	gormDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := gormDB.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1) // one connection, so every query sees the same in-memory database
	require.NoError(t, gormDB.AutoMigrate(tables...))
	return &database.Database{DB: gormDB, SqlDB: sqlDB}
}

func newAccessEnv(t *testing.T) *accessEnv {
	t.Helper()
	t.Setenv("JWT_SECRET", testSecret)
	gin.SetMode(gin.TestMode)

	env := &accessEnv{
		router:  gin.New(),
		db:      memoryDB(t, &models.User{}, &models.Lab{}, &models.LabSpec{}, &models.Session{}),
		auditDB: memoryDB(t, &models.AuditLog{}),
		users:   map[string]models.User{},
		tokens:  map[string]string{},
	}
	gormDB := env.db.DB
	store := &audit.Store{Writer: audit.NewDBRecorder(env.auditDB), Reader: env.auditDB}
	api.SetupRoutes(env.router, env.db, nil, websocket.NewManager(), store)

	hash, err := auth.HashPassword("Correct-Horse-9!")
	require.NoError(t, err)
	for _, role := range []string{"student", "instructor", "admin"} {
		user := models.User{ID: uuid.New(), Username: role + "1", Email: role + "@example.com", PasswordHash: hash, Role: role, IsActive: true}
		require.NoError(t, gormDB.Create(&user).Error)
		pair, err := auth.GenerateTokenPair(user.ID, user.Username, user.Email, user.Role, testSecret)
		require.NoError(t, err)
		env.users[role], env.tokens[role] = user, pair.AccessToken
	}
	return env
}

// role "" sends no token.
func (e *accessEnv) do(method, path, role, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "audit-test/1.0")
	req.RemoteAddr = "203.0.113.7:4711"
	if role != "" {
		req.Header.Set("Authorization", "Bearer "+e.tokens[role])
	}
	w := httptest.NewRecorder()
	e.router.ServeHTTP(w, req)
	return w
}

func (e *accessEnv) entries(t *testing.T, where string, args ...interface{}) []models.AuditLog {
	t.Helper()
	var out []models.AuditLog
	require.NoError(t, e.auditDB.DB.Where(where, args...).Order("created_at").Find(&out).Error)
	return out
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

const newLab = `{"name":"Access test lab","slug":"access-test-lab"}`

func TestAccessControl_PermissionPerRole(t *testing.T) {
	env := newAccessEnv(t)
	someID := uuid.New().String()

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		// Status per role. 404 and 400 mean the request got past the permission check.
		student, instructor, admin int
	}{
		{"list labs", "GET", "/api/v1/labs/", "", 200, 200, 200},
		{"create a lab", "POST", "/api/v1/labs/", `{"name":"x"}`, 403, 400, 400},
		{"update a lab", "PUT", "/api/v1/labs/" + someID, `{}`, 403, 404, 404},
		{"delete a lab", "DELETE", "/api/v1/labs/" + someID, "", 403, 404, 404},
		{"read lab specs", "GET", "/api/v1/labs/" + someID + "/specs", "", 200, 200, 200},
		{"create a lab spec", "POST", "/api/v1/labs/" + someID + "/specs", `{}`, 403, 404, 404},
		{"delete a lab spec", "DELETE", "/api/v1/labs/" + someID + "/specs/1", "", 403, 404, 404},
		{"set any session's status", "PUT", "/api/v1/sessions/" + someID + "/status", `{"status":"completed"}`, 403, 403, 200},
		{"list own sessions", "GET", "/api/v1/sessions/", "", 200, 200, 200},
		{"read own profile", "GET", "/api/v1/users/profile", "", 200, 200, 200},
		{"list all users", "GET", "/api/v1/admin/users", "", 403, 403, 200},
		{"change a role", "PUT", "/api/v1/admin/users/" + someID + "/role", `{"role":"instructor"}`, 403, 403, 404},
		{"change a status", "PUT", "/api/v1/admin/users/" + someID + "/status", `{"is_active":false}`, 403, 403, 404},
		{"read the audit log", "GET", "/api/v1/admin/audit-logs", "", 403, 403, 200},
	}
	for _, tt := range tests {
		for role, want := range map[string]int{"student": tt.student, "instructor": tt.instructor, "admin": tt.admin} {
			t.Run(tt.name+" as "+role, func(t *testing.T) {
				w := env.do(tt.method, tt.path, role, tt.body)
				if want == 403 {
					assert.Equal(t, 403, w.Code, w.Body.String())
				} else {
					assert.NotEqual(t, 403, w.Code, "%s should be allowed: %s", role, w.Body.String())
					assert.NotEqual(t, 401, w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestAccessControl_OwnRecordsOnly(t *testing.T) {
	env := newAccessEnv(t)

	// The instructor's lab
	w := env.do("POST", "/api/v1/labs/", "instructor", newLab)
	require.Equal(t, 201, w.Code, w.Body.String())
	var created struct {
		Lab models.Lab `json:"lab"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
	labPath := "/api/v1/labs/" + created.Lab.ID.String()

	// A second instructor can't change it; the owner and an admin can
	other := models.User{ID: uuid.New(), Username: "instructor2", Email: "i2@example.com", PasswordHash: "x", Role: "instructor", IsActive: true}
	require.NoError(t, env.db.DB.Create(&other).Error)
	pair, err := auth.GenerateTokenPair(other.ID, other.Username, other.Email, other.Role, testSecret)
	require.NoError(t, err)
	env.tokens["instructor2"] = pair.AccessToken

	assert.Equal(t, 403, env.do("PUT", labPath, "instructor2", `{"name":"Taken over"}`).Code)
	assert.Equal(t, 403, env.do("DELETE", labPath, "instructor2", "").Code)
	assert.Equal(t, 200, env.do("PUT", labPath, "instructor", `{"name":"Renamed by owner"}`).Code)
	assert.Equal(t, 200, env.do("PUT", labPath, "admin", `{"name":"Renamed by admin"}`).Code)
}

// A change to a user's role or status applies to the next request, not when the token expires.
func TestAccessControl_RoleAndStatusAreCurrent(t *testing.T) {
	env := newAccessEnv(t)

	// The token says instructor; the database now says student
	require.NoError(t, env.db.DB.Model(&models.User{}).Where("id = ?", env.users["instructor"].ID).Update("role", "student").Error)
	assert.Equal(t, 403, env.do("POST", "/api/v1/labs/", "instructor", newLab).Code, "a demoted user must lose the role at once")

	// The token says student; the database now says instructor
	require.NoError(t, env.db.DB.Model(&models.User{}).Where("id = ?", env.users["student"].ID).Update("role", "instructor").Error)
	assert.Equal(t, 201, env.do("POST", "/api/v1/labs/", "student", newLab).Code, "a promoted user must gain the role at once")

	// Deactivated and deleted users are refused with a token that is still valid
	require.NoError(t, env.db.DB.Model(&models.User{}).Where("id = ?", env.users["admin"].ID).Update("is_active", false).Error)
	assert.Equal(t, 401, env.do("GET", "/api/v1/admin/users", "admin", "").Code)
	require.NoError(t, env.db.DB.Delete(&models.User{}, "id = ?", env.users["student"].ID).Error)
	assert.Equal(t, 401, env.do("GET", "/api/v1/labs/", "student", "").Code)
}

func TestAudit_WhoDidWhatFromWhereAndWhen(t *testing.T) {
	env := newAccessEnv(t)

	t.Run("a refused request", func(t *testing.T) {
		require.Equal(t, 403, env.do("POST", "/api/v1/labs/", "student", newLab).Code)
		got := env.entries(t, "action = ? AND outcome = ?", "labs:create", "denied")
		require.Len(t, got, 1)
		e := got[0]
		require.NotNil(t, e.UserID)
		assert.Equal(t, env.users["student"].ID, *e.UserID)
		assert.Equal(t, "student1", deref(e.ActorUsername))
		assert.Equal(t, "student", deref(e.ActorRole))
		assert.Equal(t, "labs", deref(e.ResourceType))
		assert.Equal(t, "POST", e.Method)
		assert.Equal(t, "/api/v1/labs/", e.Path)
		assert.Equal(t, 403, e.StatusCode)
		assert.Equal(t, "203.0.113.7", deref(e.IPAddress))
		assert.Equal(t, "audit-test/1.0", deref(e.UserAgent))
		assert.False(t, e.CreatedAt.IsZero())
	})

	t.Run("a change, with the record it made", func(t *testing.T) {
		w := env.do("POST", "/api/v1/labs/", "instructor", newLab)
		require.Equal(t, 201, w.Code, w.Body.String())
		var created struct {
			Lab models.Lab `json:"lab"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &created))
		got := env.entries(t, "action = ? AND outcome = ?", "labs:create", "success")
		require.Len(t, got, 1)
		assert.Equal(t, created.Lab.ID.String(), deref(got[0].ResourceID))
		assert.Equal(t, env.users["instructor"].ID, *got[0].UserID)
		require.NotNil(t, got[0].NewValues)
		assert.JSONEq(t, `{"name":"Access test lab","slug":"access-test-lab"}`, string(*got[0].NewValues))
	})

	t.Run("a role change, with the old and new role", func(t *testing.T) {
		target := env.users["student"].ID.String()
		require.Equal(t, 200, env.do("PUT", "/api/v1/admin/users/"+target+"/role", "admin", `{"role":"instructor"}`).Code)
		got := env.entries(t, "action = ?", "users:update_role")
		require.Len(t, got, 1)
		assert.Equal(t, "success", got[0].Outcome)
		assert.Equal(t, env.users["admin"].ID, *got[0].UserID)
		assert.Equal(t, "users", deref(got[0].ResourceType))
		assert.Equal(t, target, deref(got[0].ResourceID))
		assert.JSONEq(t, `{"role":"student"}`, string(*got[0].OldValues))
		assert.JSONEq(t, `{"role":"instructor"}`, string(*got[0].NewValues))
	})

	t.Run("a status change, with the old and new status", func(t *testing.T) {
		target := env.users["instructor"].ID.String()
		require.Equal(t, 200, env.do("PUT", "/api/v1/admin/users/"+target+"/status", "admin", `{"is_active":false}`).Code)
		got := env.entries(t, "action = ?", "users:update_status")
		require.Len(t, got, 1)
		assert.JSONEq(t, `{"is_active":true}`, string(*got[0].OldValues))
		assert.JSONEq(t, `{"is_active":false}`, string(*got[0].NewValues))
	})

	t.Run("a request without a token", func(t *testing.T) {
		require.Equal(t, 401, env.do("GET", "/api/v1/admin/users", "", "").Code)
		got := env.entries(t, "path = ? AND status_code = ?", "/api/v1/admin/users", 401)
		require.Len(t, got, 1)
		assert.Equal(t, "denied", got[0].Outcome)
		assert.Nil(t, got[0].UserID)
		assert.Equal(t, "203.0.113.7", deref(got[0].IPAddress))
	})

	t.Run("logins, and never the password", func(t *testing.T) {
		require.Equal(t, 401, env.do("POST", "/api/v1/auth/login", "", `{"username":"admin1","password":"Wrong-Guess-1!"}`).Code)
		require.Equal(t, 401, env.do("POST", "/api/v1/auth/login", "", `{"username":"nobody","password":"Wrong-Guess-2!"}`).Code)
		require.Equal(t, 200, env.do("POST", "/api/v1/auth/login", "", `{"username":"admin1","password":"Correct-Horse-9!"}`).Code)

		got := env.entries(t, "action = ?", "auth:login")
		require.Len(t, got, 3)
		assert.Equal(t, "denied", got[0].Outcome)
		assert.Equal(t, env.users["admin"].ID, *got[0].UserID, "a wrong password for a real user names the user")
		assert.Equal(t, "denied", got[1].Outcome)
		assert.Nil(t, got[1].UserID, "an unknown username has no user")
		assert.JSONEq(t, `{"username":"nobody"}`, string(*got[1].Metadata))
		assert.Equal(t, "success", got[2].Outcome)
		assert.Equal(t, env.users["admin"].ID, *got[2].UserID)
		assert.Equal(t, "admin1", deref(got[2].ActorUsername))

		// Nothing in the whole log holds a password
		var all []models.AuditLog
		require.NoError(t, env.auditDB.DB.Find(&all).Error)
		raw, err := json.Marshal(all)
		require.NoError(t, err)
		for _, secret := range []string{"Wrong-Guess-1!", "Wrong-Guess-2!", "Correct-Horse-9!"} {
			assert.False(t, strings.Contains(string(raw), secret), "the audit log contains a password")
		}
	})
}

func TestAudit_WhatIsRecorded(t *testing.T) {
	env := newAccessEnv(t)

	count := func() int64 {
		var n int64
		require.NoError(t, env.auditDB.DB.Model(&models.AuditLog{}).Count(&n).Error)
		return n
	}

	// An ordinary successful read isn't recorded
	before := count()
	require.Equal(t, 200, env.do("GET", "/api/v1/labs/", "student", "").Code)
	assert.Equal(t, before, count(), "a successful read of the lab list is not an audit event")

	// A read of other people's data is, and so is reading the log itself
	require.Equal(t, 200, env.do("GET", "/api/v1/admin/users", "admin", "").Code)
	require.Equal(t, 200, env.do("GET", "/api/v1/admin/audit-logs", "admin", "").Code)
	assert.Len(t, env.entries(t, "action = ? AND outcome = ?", "users:list", "success"), 1)
	assert.Len(t, env.entries(t, "action = ? AND outcome = ?", "audit:read", "success"), 1)

	// CORS preflights aren't
	before = count()
	env.do("OPTIONS", "/api/v1/labs/", "", "")
	assert.Equal(t, before, count())
}

func TestAudit_ReadsAreRecordedWhenTurnedOn(t *testing.T) {
	t.Setenv("AUDIT_READS", "true")
	env := newAccessEnv(t)
	require.Equal(t, 200, env.do("GET", "/api/v1/labs/", "student", "").Code)
	got := env.entries(t, "action = ?", "labs:read")
	require.Len(t, got, 1)
	assert.Equal(t, env.users["student"].ID, *got[0].UserID)
}

func TestAudit_QueryAndNoWayToChange(t *testing.T) {
	env := newAccessEnv(t)
	require.Equal(t, 403, env.do("POST", "/api/v1/labs/", "student", newLab).Code)
	require.Equal(t, 201, env.do("POST", "/api/v1/labs/", "instructor", newLab).Code)
	require.Equal(t, 401, env.do("POST", "/api/v1/auth/login", "", `{"username":"nobody","password":"x"}`).Code)

	list := func(query string) (int, []models.AuditLog) {
		w := env.do("GET", "/api/v1/admin/audit-logs"+query, "admin", "")
		var resp struct {
			AuditLogs  []models.AuditLog `json:"audit_logs"`
			Pagination struct {
				Total int `json:"total"`
			} `json:"pagination"`
		}
		require.Equal(t, 200, w.Code, w.Body.String())
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return resp.Pagination.Total, resp.AuditLogs
	}

	total, _ := list("?outcome=denied")
	assert.Equal(t, 2, total, "the refused lab and the failed login")

	total, got := list("?action=labs:create&outcome=success")
	require.Equal(t, 1, total)
	assert.Equal(t, env.users["instructor"].ID, *got[0].UserID)

	total, _ = list("?user_id=" + env.users["student"].ID.String())
	assert.Equal(t, 1, total)

	total, _ = list("?resource_type=labs")
	assert.Equal(t, 2, total)

	total, _ = list("?ip_address=203.0.113.7&from=2000-01-01T00:00:00Z")
	assert.GreaterOrEqual(t, total, 3)

	total, _ = list("?to=2000-01-01T00:00:00Z")
	assert.Equal(t, 0, total)

	assert.Equal(t, 400, env.do("GET", "/api/v1/admin/audit-logs?from=yesterday", "admin", "").Code)
	assert.Equal(t, 400, env.do("GET", "/api/v1/admin/audit-logs?user_id=nope", "admin", "").Code)

	// The API has no route that changes or removes an entry
	id := got[0].ID.String()
	for _, method := range []string{"PUT", "PATCH", "DELETE", "POST"} {
		for _, path := range []string{"/api/v1/admin/audit-logs", "/api/v1/admin/audit-logs/" + id} {
			code := env.do(method, path, "admin", `{}`).Code
			assert.Contains(t, []int{404, 405}, code, "%s %s", method, path)
		}
	}
}

// A change that can't be recorded doesn't happen (owner decision, 2026-10-01).
func TestAudit_ChangeIsRefusedWhenItCannotBeRecorded(t *testing.T) {
	env := newAccessEnv(t)
	labs := func() int64 {
		var n int64
		require.NoError(t, env.db.DB.Model(&models.Lab{}).Count(&n).Error)
		return n
	}

	// Working audit log: the change has an attempted entry and an outcome entry, tied together
	require.Equal(t, 201, env.do("POST", "/api/v1/labs/", "instructor", newLab).Code)
	result := env.entries(t, "action = ? AND outcome = ?", "labs:create", "success")
	require.Len(t, result, 1)
	require.NotNil(t, result[0].RequestID)
	pair := env.entries(t, "request_id = ?", *result[0].RequestID)
	require.Len(t, pair, 2)
	assert.Equal(t, "attempted", pair[0].Outcome)
	assert.Equal(t, "POST /api/v1/labs/", pair[0].Action)
	assert.Equal(t, "203.0.113.7", deref(pair[0].IPAddress))
	assert.Equal(t, "success", pair[1].Outcome)

	// Break the audit store
	require.NoError(t, env.auditDB.DB.Exec("DROP TABLE audit_logs").Error)
	before := labs()

	w := env.do("POST", "/api/v1/labs/", "instructor", `{"name":"Unrecorded lab","slug":"unrecorded-lab"}`)
	assert.Equal(t, 503, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "audit log is unavailable")
	assert.Equal(t, before, labs(), "the lab must not be created when the change can't be recorded")

	target := env.users["student"].ID
	assert.Equal(t, 503, env.do("PUT", "/api/v1/admin/users/"+target.String()+"/role", "admin", `{"role":"admin"}`).Code)
	var student models.User
	require.NoError(t, env.db.DB.First(&student, "id = ?", target).Error)
	assert.Equal(t, "student", student.Role, "the role must not change when the change can't be recorded")

	assert.Equal(t, 503, env.do("POST", "/api/v1/auth/login", "", `{"username":"admin1","password":"Correct-Horse-9!"}`).Code)

	// Reads still work
	assert.Equal(t, 200, env.do("GET", "/api/v1/labs/", "student", "").Code)
}

func TestAudit_NotRequiredLetsTheChangeThrough(t *testing.T) {
	t.Setenv("AUDIT_REQUIRED", "false")
	env := newAccessEnv(t)

	// Not required: one entry per change, no attempted entry
	require.Equal(t, 201, env.do("POST", "/api/v1/labs/", "instructor", newLab).Code)
	assert.Len(t, env.entries(t, "outcome = ?", "attempted"), 0)
	assert.Len(t, env.entries(t, "action = ?", "labs:create"), 1)

	require.NoError(t, env.auditDB.DB.Exec("DROP TABLE audit_logs").Error)
	w := env.do("POST", "/api/v1/labs/", "instructor", `{"name":"Unrecorded lab","slug":"unrecorded-lab"}`)
	assert.Equal(t, 201, w.Code, w.Body.String())
}

// The audit log is not in the app's database, and survives what happens there.
func TestAudit_IsApartFromAppData(t *testing.T) {
	env := newAccessEnv(t)
	assert.False(t, env.db.DB.Migrator().HasTable("audit_logs"), "the app's database must have no audit table")

	target := env.users["student"]
	require.Equal(t, 200, env.do("PUT", "/api/v1/admin/users/"+target.ID.String()+"/role", "admin", `{"role":"instructor"}`).Code)

	// Remove both users from the app's database: the entry still says who did what to whom
	require.NoError(t, env.db.DB.Exec("DELETE FROM users").Error)
	got := env.entries(t, "action = ?", "users:update_role")
	require.Len(t, got, 1)
	assert.Equal(t, env.users["admin"].ID, *got[0].UserID)
	assert.Equal(t, "admin1", deref(got[0].ActorUsername))
	assert.Equal(t, "admin", deref(got[0].ActorRole))
	assert.Equal(t, target.ID.String(), deref(got[0].ResourceID))
}

// The admin endpoint is off, not broken, when the read-only connection isn't configured.
func TestAudit_ReadingNeedsTheReadConnection(t *testing.T) {
	env := newAccessEnv(t)
	router := gin.New()
	api.SetupRoutes(router, env.db, nil, websocket.NewManager(), &audit.Store{Writer: audit.NewDBRecorder(env.auditDB)})
	env.router = router
	w := env.do("GET", "/api/v1/admin/audit-logs", "admin", "")
	assert.Equal(t, 503, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "AUDIT_READ_DATABASE_URL")
}

// With a spool, an unreachable store doesn't stop the API: entries wait on disk and are
// delivered when the store is back. Only when the spool can't be written either is a change
// refused.
func TestAudit_SpoolCarriesTheAPIThroughAStoreOutage(t *testing.T) {
	env := newAccessEnv(t)
	dir := t.TempDir()
	spool, err := audit.NewSpool(audit.NewDBRecorder(env.auditDB), dir)
	require.NoError(t, err)
	router := gin.New()
	api.SetupRoutes(router, env.db, nil, websocket.NewManager(), &audit.Store{Writer: spool, Reader: env.auditDB})
	env.router = router

	// The store goes away
	require.NoError(t, env.auditDB.DB.Exec("ALTER TABLE audit_logs RENAME TO audit_logs_away").Error)

	// Changes still work, and their entries wait in the spool
	require.Equal(t, 201, env.do("POST", "/api/v1/labs/", "instructor", newLab).Code)
	require.Equal(t, 403, env.do("POST", "/api/v1/labs/", "student", newLab).Code)
	pending := spool.Pending()
	assert.Len(t, pending, 4, "an attempted and an outcome entry for each of the two requests")

	// While the store is away, forwarding delivers nothing and loses nothing
	n, err := spool.Forward()
	assert.Equal(t, 0, n)
	assert.Error(t, err)
	assert.Len(t, spool.Pending(), 4)

	// The store comes back: everything is delivered, in order, with its original time
	require.NoError(t, env.auditDB.DB.Exec("ALTER TABLE audit_logs_away RENAME TO audit_logs").Error)
	n, err = spool.Forward()
	require.NoError(t, err)
	assert.Equal(t, 4, n)
	assert.Len(t, spool.Pending(), 0)
	created := env.entries(t, "action = ? AND outcome = ?", "labs:create", "success")
	require.Len(t, created, 1)
	assert.Equal(t, env.users["instructor"].ID, *created[0].UserID)
	assert.Len(t, env.entries(t, "action = ? AND outcome = ?", "labs:create", "denied"), 1)
	assert.Len(t, env.entries(t, "outcome = ?", "attempted"), 2)

	// Delivering an entry twice (a crash between delivery and clean-up) doesn't duplicate it
	require.NoError(t, spool.Record(&created[0]))
	var total int64
	require.NoError(t, env.auditDB.DB.Model(&models.AuditLog{}).Where("id = ?", created[0].ID).Count(&total).Error)
	assert.Equal(t, int64(1), total)

	// Neither the store nor the spool can be written: the change is refused
	require.NoError(t, env.auditDB.DB.Exec("ALTER TABLE audit_logs RENAME TO audit_logs_away").Error)
	require.NoError(t, os.RemoveAll(dir))
	var labsBefore int64
	require.NoError(t, env.db.DB.Model(&models.Lab{}).Count(&labsBefore).Error)
	w := env.do("POST", "/api/v1/labs/", "instructor", `{"name":"Unrecorded lab","slug":"unrecorded-lab"}`)
	assert.Equal(t, 503, w.Code, w.Body.String())
	var labsAfter int64
	require.NoError(t, env.db.DB.Model(&models.Lab{}).Count(&labsAfter).Error)
	assert.Equal(t, labsBefore, labsAfter)
}

// An instructor writes specs only for their own labs; an admin for any.
func TestLabSpecs_OwnLabsOnly(t *testing.T) {
	env := newAccessEnv(t)
	owner := env.users["instructor"].ID
	lab := models.Lab{ID: uuid.New(), Name: "Owned lab", Slug: "owned-lab", IsPublished: true, CreatedBy: &owner}
	orphan := models.Lab{ID: uuid.New(), Name: "Lab without a creator", Slug: "orphan-lab", IsPublished: true}
	require.NoError(t, env.db.DB.Create(&lab).Error)
	require.NoError(t, env.db.DB.Create(&orphan).Error)

	other := models.User{ID: uuid.New(), Username: "instructor2", Email: "i2@example.com", PasswordHash: "x", Role: "instructor", IsActive: true}
	require.NoError(t, env.db.DB.Create(&other).Error)
	pair, err := auth.GenerateTokenPair(other.ID, other.Username, other.Email, other.Role, testSecret)
	require.NoError(t, err)
	env.tokens["instructor2"] = pair.AccessToken

	specs := "/api/v1/labs/" + lab.ID.String() + "/specs"
	specCount := func() int64 {
		var n int64
		require.NoError(t, env.db.DB.Model(&models.LabSpec{}).Where("lab_id = ?", lab.ID).Count(&n).Error)
		return n
	}
	// A spec to act on, put there directly
	require.NoError(t, env.db.DB.Exec("INSERT INTO lab_specs (id, lab_id, version, specification, is_active) VALUES (?, ?, 1, '{}', true)", uuid.New(), lab.ID).Error)

	tests := []struct {
		name   string
		method string
		path   string
		// 403 means refused; anything else means the ownership check let it through
		owner, otherInstructor, admin, student bool
	}{
		{"create a spec", "POST", specs, true, false, true, false},
		{"update a spec", "PUT", specs + "/1", true, false, true, false},
		{"delete a spec", "DELETE", specs + "/99", true, false, true, false},
	}
	for _, tt := range tests {
		for role, allowed := range map[string]bool{"instructor": tt.owner, "instructor2": tt.otherInstructor, "admin": tt.admin, "student": tt.student} {
			t.Run(tt.name+" as "+role, func(t *testing.T) {
				before := specCount()
				w := env.do(tt.method, tt.path, role, `{}`)
				if allowed {
					assert.NotEqual(t, 403, w.Code, w.Body.String())
				} else {
					assert.Equal(t, 403, w.Code, w.Body.String())
					assert.Equal(t, before, specCount(), "a refused request must not change the specs")
				}
			})
		}
	}

	// Reading is still open to everyone
	assert.Equal(t, 200, env.do("GET", specs, "instructor2", "").Code)
	assert.Equal(t, 200, env.do("GET", specs, "student", "").Code)

	// A lab that doesn't exist: 404, and no spec is created for it
	missing := "/api/v1/labs/" + uuid.NewString() + "/specs"
	assert.Equal(t, 404, env.do("POST", missing, "instructor", `{}`).Code)
	assert.Equal(t, 404, env.do("POST", missing, "admin", `{}`).Code)

	// A lab without a creator belongs to nobody: admins only
	orphanSpecs := "/api/v1/labs/" + orphan.ID.String() + "/specs"
	assert.Equal(t, 403, env.do("POST", orphanSpecs, "instructor", `{}`).Code)
	assert.NotEqual(t, 403, env.do("POST", orphanSpecs, "admin", `{}`).Code)

	// The refusal is in the audit log, against the lab
	denied := env.entries(t, "action = ? AND outcome = ? AND resource_id = ?", "lab_specs:write", "denied", lab.ID.String())
	assert.NotEmpty(t, denied)
}
