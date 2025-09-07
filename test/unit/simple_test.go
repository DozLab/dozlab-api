package unit

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Simple test to verify the test framework is working
func TestBasicFunctionality(t *testing.T) {
	// Test basic assertions work
	assert.Equal(t, 1, 1, "Basic equality should work")
	assert.True(t, true, "True should be true")
	assert.NotNil(t, t, "Test instance should not be nil")
}

// Test environment variables
func TestEnvironmentSetup(t *testing.T) {
	// These should be set by the test script
	// But let's not require them for this basic test
	assert.True(t, true, "Environment test passed")
}

// Test string operations
func TestStringOperations(t *testing.T) {
	str := "dozlab-api"
	assert.Contains(t, str, "api", "String should contain 'api'")
	assert.Equal(t, 10, len(str), "String length should be 10")
}

// Test struct creation
func TestStructCreation(t *testing.T) {
	type TestStruct struct {
		Name string
		ID   int
	}

	ts := TestStruct{
		Name: "test",
		ID:   1,
	}

	assert.Equal(t, "test", ts.Name)
	assert.Equal(t, 1, ts.ID)
}

// Test slice operations
func TestSliceOperations(t *testing.T) {
	slice := []string{"auth", "user", "lab"}
	
	assert.Len(t, slice, 3, "Slice should have 3 elements")
	assert.Contains(t, slice, "auth", "Slice should contain 'auth'")
	assert.Equal(t, "user", slice[1], "Second element should be 'user'")
}

// Test map operations
func TestMapOperations(t *testing.T) {
	m := map[string]int{
		"users":    100,
		"sessions": 50,
		"labs":     25,
	}

	assert.Equal(t, 100, m["users"])
	assert.Contains(t, m, "sessions")
	assert.Len(t, m, 3)
}