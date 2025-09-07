# Dozlab API Endpoints

## Authentication Endpoints

### POST /api/v1/auth/register
Register a new user account.

**Request Body:**
```json
{
  "username": "string",
  "email": "string", 
  "password": "string",
  "first_name": "string",
  "last_name": "string"
}
```

**Response:** `201 Created`
```json
{
  "message": "User registered successfully",
  "user": {
    "id": "uuid",
    "username": "string",
    "email": "string",
    "first_name": "string", 
    "last_name": "string",
    "role": "student",
    "is_active": true,
    "created_at": "timestamp",
    "updated_at": "timestamp"
  },
  "tokens": {
    "access_token": "jwt",
    "refresh_token": "jwt"
  }
}
```

### POST /api/v1/auth/login
Authenticate user credentials.

**Request Body:**
```json
{
  "username": "string",
  "password": "string"
}
```

**Response:** `200 OK`
```json
{
  "message": "Login successful",
  "user": {
    "id": "uuid",
    "username": "string",
    "email": "string",
    "role": "string",
    "last_login_at": "timestamp"
  },
  "tokens": {
    "access_token": "jwt",
    "refresh_token": "jwt"
  }
}
```

### POST /api/v1/auth/refresh
Refresh access token using refresh token.

**Request Body:**
```json
{
  "refresh_token": "jwt"
}
```

**Response:** `200 OK`
```json
{
  "message": "Token refreshed successfully",
  "tokens": {
    "access_token": "jwt", 
    "refresh_token": "jwt"
  }
}
```

## User Endpoints (Protected)

### GET /api/v1/users/profile
Get current user profile.

**Headers:** `Authorization: Bearer <access_token>`

**Response:** `200 OK`
```json
{
  "id": "uuid",
  "username": "string",
  "email": "string",
  "first_name": "string",
  "last_name": "string", 
  "role": "string",
  "is_active": true,
  "created_at": "timestamp",
  "updated_at": "timestamp"
}
```

### PUT /api/v1/users/profile
Update current user profile.

**Headers:** `Authorization: Bearer <access_token>`

**Request Body:**
```json
{
  "first_name": "string",
  "last_name": "string",
  "email": "string"
}
```

### POST /api/v1/users/logout
Logout current user.

**Headers:** `Authorization: Bearer <access_token>`

**Response:** `200 OK`

## Lab Session Endpoints (Protected)

### POST /api/v1/lab-sessions
Create a new lab session using Kubernetes CRD.

**Headers:** `Authorization: Bearer <access_token>`

**Request Body:**
```json
{
  "lab_id": "uuid",
  "spec_version": "string"
}
```

### GET /api/v1/lab-sessions
List user's lab sessions.

**Headers:** `Authorization: Bearer <access_token>`

### GET /api/v1/lab-sessions/{id}
Get specific lab session details.

**Headers:** `Authorization: Bearer <access_token>`

### DELETE /api/v1/lab-sessions/{id}
Delete a lab session.

**Headers:** `Authorization: Bearer <access_token>`

## Admin Endpoints (Protected - Admin Role Required)

### GET /api/v1/admin/users
List all users (admin only).

### PUT /api/v1/admin/users/{id}/role
Update user role (admin only).

### PUT /api/v1/admin/users/{id}/status
Update user status (admin only).

## Health Check

### GET /health
Service health check endpoint.

**Response:** `200 OK`
```json
{
  "status": "ok",
  "message": "Dozlab Backend is healthy"
}
```