package integration

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"dozlab-backend/internal/models"
	"dozlab-backend/pkg/auth"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Refreshing gives tokens for the user as they are now, and none to a user who is gone.
func TestRefresh_UsesTheCurrentUser(t *testing.T) {
	env := newAccessEnv(t)
	refreshTokenFor := func(role string) string {
		u := env.users[role]
		pair, err := auth.GenerateTokenPair(u.ID, u.Username, u.Email, u.Role, testSecret)
		require.NoError(t, err)
		return pair.RefreshToken
	}
	refresh := func(token string) (int, *auth.JWTClaims, string) {
		w := env.do("POST", "/api/v1/auth/refresh", "", `{"refresh_token":"`+token+`"}`)
		if w.Code != 200 {
			return w.Code, nil, w.Body.String()
		}
		var resp struct {
			Tokens auth.TokenPair `json:"tokens"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		claims, err := auth.ValidateToken(resp.Tokens.AccessToken, testSecret)
		require.NoError(t, err)
		refreshClaims, err := auth.ValidateRefreshToken(resp.Tokens.RefreshToken, testSecret)
		require.NoError(t, err)
		assert.Equal(t, claims.Role, refreshClaims.Role)
		return w.Code, claims, ""
	}

	// Unchanged user: new tokens with the same identity
	instructorToken := refreshTokenFor("instructor")
	code, claims, _ := refresh(instructorToken)
	require.Equal(t, 200, code)
	assert.Equal(t, env.users["instructor"].ID, claims.UserID)
	assert.Equal(t, "instructor", claims.Role)

	// Demoted after the token was issued: the new tokens carry the new role, and the new name
	require.NoError(t, env.db.DB.Model(&models.User{}).Where("id = ?", env.users["instructor"].ID).
		Updates(map[string]interface{}{"role": "student", "username": "renamed1", "email": "renamed@example.com"}).Error)
	code, claims, _ = refresh(instructorToken)
	require.Equal(t, 200, code)
	assert.Equal(t, "student", claims.Role)
	assert.Equal(t, "renamed1", claims.Username)
	assert.Equal(t, "renamed@example.com", claims.Email)

	// Promoted: likewise
	studentToken := refreshTokenFor("student")
	require.NoError(t, env.db.DB.Model(&models.User{}).Where("id = ?", env.users["student"].ID).Update("role", "instructor").Error)
	code, claims, _ = refresh(studentToken)
	require.Equal(t, 200, code)
	assert.Equal(t, "instructor", claims.Role)

	// Deactivated: no new tokens
	adminToken := refreshTokenFor("admin")
	require.NoError(t, env.db.DB.Model(&models.User{}).Where("id = ?", env.users["admin"].ID).Update("is_active", false).Error)
	code, _, body := refresh(adminToken)
	assert.Equal(t, 401, code)
	assert.Contains(t, body, "inactive")
	assert.NotContains(t, body, "access_token")

	// Deleted: no new tokens
	require.NoError(t, env.db.DB.Delete(&models.User{}, "id = ?", env.users["student"].ID).Error)
	code, _, body = refresh(studentToken)
	assert.Equal(t, 401, code)
	assert.Contains(t, body, "no longer exists")

	// A token that isn't one, or is signed with another secret
	code, _, _ = refresh("not-a-token")
	assert.Equal(t, 401, code)
	forged, err := auth.GenerateTokenPair(env.users["instructor"].ID, "x", "x@example.com", "admin", "another-secret-key-32-characters!!")
	require.NoError(t, err)
	code, _, _ = refresh(forged.RefreshToken)
	assert.Equal(t, 401, code)

	// An access token doesn't refresh
	code, _, _ = refresh(env.tokens["instructor"])
	assert.Equal(t, 401, code)

	// The refusals are in the audit log, with who tried
	denied := env.entries(t, "action = ? AND outcome = ?", "auth:refresh", "denied")
	require.Len(t, denied, 5)
	require.NotNil(t, denied[0].UserID)
	assert.Equal(t, env.users["admin"].ID, *denied[0].UserID)
}

// A refresh token doesn't authenticate requests: it only gets new tokens.
func TestRefreshTokenIsNotABearerToken(t *testing.T) {
	env := newAccessEnv(t)
	u := env.users["instructor"]
	pair, err := auth.GenerateTokenPair(u.ID, u.Username, u.Email, u.Role, testSecret)
	require.NoError(t, err)

	get := func(token string) int {
		req := httptest.NewRequest("GET", "/api/v1/users/profile", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		w := httptest.NewRecorder()
		env.router.ServeHTTP(w, req)
		return w.Code
	}
	assert.Equal(t, 200, get(pair.AccessToken))
	assert.Equal(t, 401, get(pair.RefreshToken))
}
