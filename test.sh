#!/bin/bash

# Dozlab API Test Script
# This script runs all tests for the dozlab-api microservice

set -e  # Exit on any error

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Test configuration
TEST_TIMEOUT="30s"
COVERAGE_FILE="coverage.out"
COVERAGE_HTML="coverage.html"

# Print colored output
print_status() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

print_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

print_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

print_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Check if we're in the right directory
if [ ! -f "go.mod" ]; then
    print_error "go.mod not found. Please run this script from the dozlab-api root directory."
    exit 1
fi

print_status "Starting Dozlab API Tests..."

# Clean up previous test artifacts
cleanup() {
    print_status "Cleaning up test artifacts..."
    rm -f $COVERAGE_FILE $COVERAGE_HTML
}

# Set up test environment
setup_test_env() {
    print_status "Setting up test environment..."
    export GO_ENV=test
    export JWT_SECRET="test-jwt-secret-key-32-characters-long-for-testing"
    export ENV=test
    
    # Ensure test dependencies are available
    go mod tidy
}

# Run unit tests
run_unit_tests() {
    print_status "Running unit tests..."
    
    if go test ./test/unit/... -v -timeout=$TEST_TIMEOUT; then
        print_success "Unit tests passed!"
        return 0
    else
        print_error "Unit tests failed!"
        return 1
    fi
}

# Run integration tests
run_integration_tests() {
    print_status "Running integration tests..."
    
    if go test ./test/integration/... -v -timeout=$TEST_TIMEOUT; then
        print_success "Integration tests passed!"
        return 0
    else
        print_error "Integration tests failed!"
        return 1
    fi
}

# Run all tests with coverage
run_tests_with_coverage() {
    print_status "Running all tests with coverage..."
    
    if go test ./test/... -v -timeout=$TEST_TIMEOUT -coverprofile=$COVERAGE_FILE -covermode=atomic; then
        print_success "All tests passed with coverage!"
        
        # Generate coverage report
        if command -v go >/dev/null 2>&1; then
            COVERAGE=$(go tool cover -func=$COVERAGE_FILE | grep total | awk '{print $3}')
            print_status "Total test coverage: $COVERAGE"
            
            # Generate HTML coverage report
            go tool cover -html=$COVERAGE_FILE -o $COVERAGE_HTML
            print_status "HTML coverage report generated: $COVERAGE_HTML"
        fi
        return 0
    else
        print_error "Tests failed!"
        return 1
    fi
}

# Run specific test suite
run_specific_tests() {
    local test_pattern=$1
    print_status "Running tests matching pattern: $test_pattern"
    
    if go test ./test/... -v -timeout=$TEST_TIMEOUT -run="$test_pattern"; then
        print_success "Specific tests passed!"
        return 0
    else
        print_error "Specific tests failed!"
        return 1
    fi
}

# Run benchmark tests (if any)
run_benchmarks() {
    print_status "Running benchmark tests..."
    
    if go test ./test/... -v -timeout=$TEST_TIMEOUT -bench=. -benchmem; then
        print_success "Benchmarks completed!"
        return 0
    else
        print_warning "No benchmark tests found or benchmarks failed"
        return 0
    fi
}

# Lint tests (basic check)
lint_tests() {
    print_status "Checking test code quality..."
    
    # Check for common issues in test files
    if find ./test -name "*.go" -exec grep -l "t.Error(" {} \; | head -1 > /dev/null; then
        print_warning "Found t.Error() usage - consider using assert library instead"
    fi
    
    # Check for missing test documentation
    if find ./test -name "*_test.go" -exec grep -L "// Test" {} \; | head -1 > /dev/null; then
        print_warning "Some test files may be missing documentation"
    fi
    
    print_success "Test code quality check completed"
}

# Show usage
show_usage() {
    echo "Usage: $0 [OPTIONS]"
    echo ""
    echo "Options:"
    echo "  -u, --unit           Run only unit tests"
    echo "  -i, --integration    Run only integration tests"
    echo "  -c, --coverage       Run all tests with coverage report"
    echo "  -b, --benchmark      Run benchmark tests"
    echo "  -l, --lint          Run test code quality checks"
    echo "  -r, --run PATTERN   Run tests matching specific pattern"
    echo "  -a, --all           Run all tests and checks (default)"
    echo "  -h, --help          Show this help message"
    echo ""
    echo "Examples:"
    echo "  $0                    # Run all tests"
    echo "  $0 -u                 # Run only unit tests"
    echo "  $0 -c                 # Run with coverage"
    echo "  $0 -r TestAuth        # Run only auth-related tests"
    echo ""
}

# Main execution
main() {
    local run_unit=false
    local run_integration=false
    local run_coverage=false
    local run_benchmark=false
    local run_lint=false
    local run_pattern=""
    local run_all=true

    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            -u|--unit)
                run_unit=true
                run_all=false
                shift
                ;;
            -i|--integration)
                run_integration=true
                run_all=false
                shift
                ;;
            -c|--coverage)
                run_coverage=true
                run_all=false
                shift
                ;;
            -b|--benchmark)
                run_benchmark=true
                run_all=false
                shift
                ;;
            -l|--lint)
                run_lint=true
                run_all=false
                shift
                ;;
            -r|--run)
                run_pattern="$2"
                run_all=false
                shift 2
                ;;
            -a|--all)
                run_all=true
                shift
                ;;
            -h|--help)
                show_usage
                exit 0
                ;;
            *)
                print_error "Unknown option: $1"
                show_usage
                exit 1
                ;;
        esac
    done

    # Setup
    setup_test_env
    
    local failed=0

    # Run tests based on options
    if [[ "$run_all" == true ]]; then
        print_status "Running complete test suite..."
        
        run_unit_tests || failed=1
        run_integration_tests || failed=1
        run_tests_with_coverage || failed=1
        lint_tests || true  # Don't fail on linting issues
        
    else
        if [[ "$run_unit" == true ]]; then
            run_unit_tests || failed=1
        fi
        
        if [[ "$run_integration" == true ]]; then
            run_integration_tests || failed=1
        fi
        
        if [[ "$run_coverage" == true ]]; then
            run_tests_with_coverage || failed=1
        fi
        
        if [[ "$run_benchmark" == true ]]; then
            run_benchmarks || failed=1
        fi
        
        if [[ "$run_lint" == true ]]; then
            lint_tests || true
        fi
        
        if [[ -n "$run_pattern" ]]; then
            run_specific_tests "$run_pattern" || failed=1
        fi
    fi

    # Cleanup and summary
    if [[ $failed -eq 0 ]]; then
        print_success "🎉 All tests completed successfully!"
        
        if [[ -f "$COVERAGE_HTML" ]]; then
            print_status "📊 Open $COVERAGE_HTML in your browser to view detailed coverage"
        fi
        
        exit 0
    else
        print_error "❌ Some tests failed!"
        cleanup
        exit 1
    fi
}

# Trap to cleanup on exit
trap cleanup EXIT

# Run main function with all arguments
main "$@"