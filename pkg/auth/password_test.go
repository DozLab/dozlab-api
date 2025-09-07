package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPassword(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{
			name:     "Valid password",
			password: "validpassword123",
			wantErr:  false,
		},
		{
			name:     "Short password",
			password: "short",
			wantErr:  false, // Hashing itself doesn't validate length
		},
		{
			name:     "Long password (within bcrypt limit)",
			password: string(make([]byte, 70)), // 70 characters - within bcrypt 72-byte limit
			wantErr:  false,
		},
		{
			name:     "Empty password",
			password: "",
			wantErr:  false, // Hashing itself doesn't validate empty
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, err := HashPassword(tt.password)
			
			if tt.wantErr {
				assert.Error(t, err)
				assert.Empty(t, hash)
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, hash)
				assert.NotEqual(t, tt.password, hash, "Hash should not equal original password")
				assert.Greater(t, len(hash), 50, "Hash should be reasonably long")
			}
		})
	}
}

func TestCheckPassword(t *testing.T) {
	password := "testpassword123"
	hash, err := HashPassword(password)
	require.NoError(t, err)
	require.NotEmpty(t, hash)

	tests := []struct {
		name     string
		password string
		hash     string
		want     bool
	}{
		{
			name:     "Correct password",
			password: password,
			hash:     hash,
			want:     true,
		},
		{
			name:     "Wrong password",
			password: "wrongpassword",
			hash:     hash,
			want:     false,
		},
		{
			name:     "Empty password",
			password: "",
			hash:     hash,
			want:     false,
		},
		{
			name:     "Invalid hash",
			password: password,
			hash:     "invalid_hash",
			want:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckPassword(tt.password, tt.hash)
			assert.Equal(t, tt.want, result)
		})
	}
}

func TestValidatePasswordStrength(t *testing.T) {
	tests := []struct {
		name     string
		password string
		wantErr  bool
		errType  error
	}{
		{
			name:     "Valid password - minimum length",
			password: "password",
			wantErr:  false,
		},
		{
			name:     "Valid password - with numbers and symbols",
			password: "StrongP@ssw0rd!",
			wantErr:  false,
		},
		{
			name:     "Too short",
			password: "short",
			wantErr:  true,
			errType:  ErrPasswordTooShort,
		},
		{
			name:     "Too long",
			password: string(make([]byte, 130)), // 130 characters
			wantErr:  true,
			errType:  ErrPasswordTooLong,
		},
		{
			name:     "Empty password",
			password: "",
			wantErr:  true,
			errType:  ErrPasswordTooShort,
		},
		{
			name:     "Exactly 8 characters",
			password: "exactly8",
			wantErr:  false,
		},
		{
			name:     "Exactly 128 characters",
			password: string(make([]byte, 128)),
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePasswordStrength(tt.password)
			
			if tt.wantErr {
				assert.Error(t, err)
				if tt.errType != nil {
					assert.Equal(t, tt.errType, err)
				}
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestHashPasswordConsistency(t *testing.T) {
	password := "consistencytest"
	
	// Generate multiple hashes of the same password
	hash1, err1 := HashPassword(password)
	hash2, err2 := HashPassword(password)
	
	require.NoError(t, err1)
	require.NoError(t, err2)
	
	// Hashes should be different (due to salt)
	assert.NotEqual(t, hash1, hash2, "Different hashes should be generated for same password")
	
	// But both should validate correctly
	assert.True(t, CheckPassword(password, hash1))
	assert.True(t, CheckPassword(password, hash2))
}