package auth

import "errors"

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUserNotFound      = errors.New("user not found")
	ErrUserExists        = errors.New("user already exists")
	ErrInvalidToken      = errors.New("invalid or expired token")
	ErrTokenExpired      = errors.New("token has expired")
	ErrPasswordTooShort  = errors.New("password must be at least 8 characters long")
	ErrPasswordTooLong   = errors.New("password must not exceed 128 characters")
	ErrInvalidEmail      = errors.New("invalid email address")
	ErrUserInactive      = errors.New("user account is inactive")
)