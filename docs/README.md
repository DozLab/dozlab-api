# Dozlab API Service

A microservice providing REST API endpoints for the Dozlab learning platform.

## Overview

The Dozlab API service handles:
- User authentication and authorization
- User profile management
- Lab session management via Kubernetes CRDs
- Admin operations

## Architecture

This service is designed as a microservice that:
- Uses environment variables for configuration
- Integrates with PostgreSQL database
- Communicates with other microservices (WebSocket, Examiner, Workflow)
- Manages Kubernetes Custom Resources for lab sessions

## Environment Variables

Required environment variables:

```bash
# Database
DATABASE_URL=postgres://user:pass@host:5432/dbname

# Authentication
JWT_SECRET=your-jwt-secret-key-32-chars-min

# Service URLs (optional - for microservice communication)
WEBSOCKET_SERVICE_URL=http://dozlab-websocket:8081
EXAMINER_SERVICE_URL=http://dozlab-examiner:8083
WORKFLOW_SERVICE_URL=http://dozlab-workflow:8084

# Redis (optional - for caching/sessions)
REDIS_ADDR=localhost:6379
REDIS_PASSWORD=
```

## Directory Structure

```
internal/
├── api/          # HTTP handlers and routes
├── database/     # Database connection and migrations
├── models/       # Data models
├── middleware/   # HTTP middleware (auth, logging, etc.)
├── services/     # Business logic services
├── clients/      # External service clients
└── ...          # Other internal packages
pkg/             # Reusable packages
docs/            # Documentation
```

## Key Features

### Authentication
- JWT-based authentication
- User registration and login
- Token refresh mechanism
- Role-based access control

### Lab Sessions
- Kubernetes CRD-based lab session management
- Integration with k8s API for resource creation
- Session lifecycle management

### Microservice Integration
- HTTP clients for other services
- Redis for shared state/caching
- Environment-based service discovery

## API Documentation

See [API Endpoints](./api/endpoints.md) for detailed endpoint documentation.

## Database Schema

The service uses the following main entities:
- Users (authentication, profiles)
- Labs (lab definitions)
- Lab Sessions (via Kubernetes CRDs)
- User Progress tracking

## Development

This service is part of a microservices architecture and should be developed with:
- Environment variable configuration
- Containerized deployment
- Health check endpoints
- Graceful shutdown handling