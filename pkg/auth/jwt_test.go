package auth

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateTokenPair(t *testing.T) {
	userID := uuid.New()
	username := "testuser"
	email := "test@example.com"
	role := "student"
	secret := "test-secret-key"

	tokens, err := GenerateTokenPair(userID, username, email, role, secret)
	
	require.NoError(t, err)
	require.NotNil(t, tokens)
	
	assert.NotEmpty(t, tokens.AccessToken)
	assert.NotEmpty(t, tokens.RefreshToken)
	assert.Greater(t, tokens.ExpiresAt, time.Now().Unix())
	assert.NotEqual(t, tokens.AccessToken, tokens.RefreshToken)
}

func TestValidateToken(t *testing.T) {
	userID := uuid.New()
	username := "testuser"
	email := "test@example.com"
	role := "admin"
	secret := "test-secret-key"

	// Generate valid token
	tokens, err := GenerateTokenPair(userID, username, email, role, secret)
	require.NoError(t, err)

	tests := []struct {
		name    string
		token   string
		secret  string
		wantErr bool
	}{
		{
			name:    "Valid access token",
			token:   tokens.AccessToken,
			secret:  secret,
			wantErr: false,
		},
		{
			name:    "Valid refresh token",
			token:   tokens.RefreshToken,
			secret:  secret,
			wantErr: false,
		},
		{
			name:    "Wrong secret",
			token:   tokens.AccessToken,
			secret:  "wrong-secret",
			wantErr: true,
		},
		{
			name:    "Invalid token format",
			token:   "invalid.token.format",
			secret:  secret,
			wantErr: true,
		},
		{
			name:    "Empty token",
			token:   "",
			secret:  secret,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			claims, err := ValidateToken(tt.token, tt.secret)
			
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, claims)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, claims)
				assert.Equal(t, userID, claims.UserID)
				assert.Equal(t, username, claims.Username)
				assert.Equal(t, email, claims.Email)
				assert.Equal(t, role, claims.Role)
			}
		})
	}
}

func TestRefreshAccessToken(t *testing.T) {
	userID := uuid.New()
	username := "testuser"
	email := "test@example.com"
	role := "instructor"
	secret := "test-secret-key"

	// Generate initial tokens
	originalTokens, err := GenerateTokenPair(userID, username, email, role, secret)
	require.NoError(t, err)

	tests := []struct {
		name         string
		refreshToken string
		secret       string
		wantErr      bool
	}{
		{
			name:         "Valid refresh token",
			refreshToken: originalTokens.RefreshToken,
			secret:       secret,
			wantErr:      false,
		},
		{
			name:         "Invalid refresh token",
			refreshToken: "invalid.token",
			secret:       secret,
			wantErr:      true,
		},
		{
			name:         "Wrong secret",
			refreshToken: originalTokens.RefreshToken,
			secret:       "wrong-secret",
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			newTokens, err := RefreshAccessToken(tt.refreshToken, tt.secret)
			
			if tt.wantErr {
				assert.Error(t, err)
				assert.Nil(t, newTokens)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, newTokens)
				assert.NotEmpty(t, newTokens.AccessToken)
				assert.NotEmpty(t, newTokens.RefreshToken)
				
				// Validate new access token
				claims, err := ValidateToken(newTokens.AccessToken, secret)
				assert.NoError(t, err)
				assert.Equal(t, userID, claims.UserID)
			}
		})
	}
}