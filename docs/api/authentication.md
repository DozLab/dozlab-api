# Authentication

## Overview

The Dozlab API uses JWT (JSON Web Tokens) for authentication and authorization.

## Token Types

### Access Token
- Short-lived token (typically 15 minutes)
- Used for API requests
- Contains user ID, username, email, and role claims

### Refresh Token
- Long-lived token (typically 7 days)  
- Used to obtain new access tokens
- Should be stored securely by client

## Authentication Flow

1. **Registration/Login**: Client sends credentials to `/auth/register` or `/auth/login`
2. **Token Response**: Server returns both access and refresh tokens
3. **API Requests**: Client includes access token in `Authorization` header
4. **Token Refresh**: When access token expires, use refresh token at `/auth/refresh`

## Headers

For protected endpoints, include the access token:

```http
Authorization: Bearer <access_token>
```

## Error Responses

### 401 Unauthorized
```json
{
  "error": "Invalid or expired token"
}
```

### 403 Forbidden
```json
{
  "error": "Insufficient permissions"
}
```

## Role-Based Access

- **student**: Default role, access to user endpoints
- **instructor**: Access to course management
- **admin**: Full system access

## Security Notes

- JWT secrets should be at least 32 characters
- Tokens should be stored securely on client side
- Use HTTPS in production
- Implement proper logout token invalidation