package integration

import (
	"testing"

	"dozlab-backend/internal/models"
	"dozlab-backend/pkg/auth"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
