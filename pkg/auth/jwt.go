package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// Token types. An access token authenticates API and WebSocket requests for an hour; a refresh
// token only gets a new pair from POST /auth/refresh, for seven days. Each is refused where the
// other belongs, and a token without a type (issued before types existed) is refused by both.
const (
	TokenTypeAccess  = "access"
	TokenTypeRefresh = "refresh"
)

// ErrWrongTokenType is returned for a valid token of the other type, or of none.
var ErrWrongTokenType = errors.New("wrong token type")

type JWTClaims struct {
	UserID    uuid.UUID `json:"user_id"`
	Username  string    `json:"username"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	TokenType string    `json:"token_type"`
	jwt.RegisteredClaims
}

type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

func GenerateTokenPair(userID uuid.UUID, username, email, role, secret string) (*TokenPair, error) {
	// Access token - expires in 1 hour
	accessClaims := &JWTClaims{
		UserID:    userID,
		Username:  username,
		Email:     email,
		Role:      role,
		TokenType: TokenTypeAccess,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 1)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "dozlab-backend",
			Subject:   userID.String(),
		},
	}

	accessToken := jwt.NewWithClaims(jwt.SigningMethodHS256, accessClaims)
	accessTokenString, err := accessToken.SignedString([]byte(secret))
	if err != nil {
		return nil, err
	}

	// Refresh token - expires in 7 days
	refreshClaims := &JWTClaims{
		UserID:    userID,
		Username:  username,
		Email:     email,
		Role:      role,
		TokenType: TokenTypeRefresh,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour * 24 * 7)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			NotBefore: jwt.NewNumericDate(time.Now()),
			Issuer:    "dozlab-backend",
			Subject:   userID.String(),
		},
	}

	refreshToken := jwt.NewWithClaims(jwt.SigningMethodHS256, refreshClaims)
	refreshTokenString, err := refreshToken.SignedString([]byte(secret))
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresAt:    time.Now().Add(time.Hour * 1).Unix(),
	}, nil
}

// ValidateToken checks an access token: the one requests authenticate with. A refresh token is
// refused (ErrWrongTokenType).
func ValidateToken(tokenString, secret string) (*JWTClaims, error) {
	return validate(tokenString, secret, TokenTypeAccess)
}

// ValidateRefreshToken checks a refresh token, for POST /auth/refresh only. An access token is
// refused (ErrWrongTokenType).
func ValidateRefreshToken(tokenString, secret string) (*JWTClaims, error) {
	return validate(tokenString, secret, TokenTypeRefresh)
}

func validate(tokenString, secret, wantType string) (*JWTClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &JWTClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*JWTClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid token")
	}
	if claims.TokenType != wantType {
		return nil, ErrWrongTokenType
	}
	return claims, nil
}

func RefreshAccessToken(refreshTokenString, secret string) (*TokenPair, error) {
	claims, err := ValidateRefreshToken(refreshTokenString, secret)
	if err != nil {
		return nil, err
	}

	// Generate new token pair
	return GenerateTokenPair(claims.UserID, claims.Username, claims.Email, claims.Role, secret)
}