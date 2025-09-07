# Tests

This directory contains comprehensive tests for the Dozlab API microservice.

## Test Structure

```
test/
├── unit/           # Unit tests for individual components
├── integration/    # Integration tests for full API flows
└── README.md       # This file
```

## Running Tests

### Prerequisites

Make sure you have the following Go packages installed:
```bash
go get github.com/stretchr/testify
go get gorm.io/driver/sqlite
go get gorm.io/gorm
```

### Unit Tests

Run unit tests for specific handlers:
```bash
# Run all unit tests
go test ./test/unit/... -v

# Run specific test file
go test ./test/unit/auth_handler_test.go -v

# Run with coverage
go test ./test/unit/... -v -cover
```

### Integration Tests

Run full API integration tests:
```bash
# Run all integration tests
go test ./test/integration/... -v

# Run with test database cleanup
go test ./test/integration/... -v -cleanup
```

### All Tests

Run all tests:
```bash
# Run everything
go test ./test/... -v

# Run with coverage report
go test ./test/... -v -coverprofile=coverage.out
go tool cover -html=coverage.out
```

## Test Categories

### Unit Tests

- **auth_handler_test.go**: Tests for authentication endpoints
  - User registration validation
  - Login functionality
  - Token refresh mechanism
  - Password strength validation
  - Duplicate user handling

- **user_handler_test.go**: Tests for user management endpoints
  - Profile retrieval
  - Profile updates
  - User progress tracking
  - Admin user operations

### Integration Tests

- **api_test.go**: Full API workflow tests
  - Complete registration → login → profile flow
  - Authentication middleware testing
  - CORS header validation
  - Database transaction testing
  - Error handling across endpoints

## Test Environment

Tests use:
- **SQLite in-memory database** for fast, isolated testing
- **Gin test mode** for HTTP handler testing
- **Test JWT secrets** for token validation
- **Mock external dependencies** where needed

## Environment Variables for Tests

The tests automatically set required environment variables:
```bash
JWT_SECRET=test-jwt-secret-key-32-characters-long
ENV=test
```

## Coverage Goals

- **Unit Tests**: Aim for 80%+ code coverage on handlers
- **Integration Tests**: Cover all major API workflows
- **Error Handling**: Test both success and failure scenarios

## Writing New Tests

### Unit Test Example
```go
func TestNewHandler_Method(t *testing.T) {
    handler := setupTestHandler()
    
    tests := []struct {
        name           string
        input          interface{}
        expectedStatus int
        expectError    bool
    }{
        // Test cases
    }
    
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            // Test implementation
        })
    }
}
```

### Integration Test Example
```go
func (suite *APITestSuite) TestNewWorkflow() {
    // Setup
    // Execute API calls
    // Verify results
    // Cleanup if needed
}
```

## Mock Data

Test data should be:
- **Realistic** but obviously fake
- **Consistent** across test runs
- **Isolated** between tests
- **Cleaned up** after each test

## Continuous Integration

Tests are designed to run in CI/CD pipelines with:
- No external dependencies
- Fast execution times
- Deterministic results
- Clear failure reporting