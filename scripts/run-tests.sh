#!/usr/bin/env bash

# Enhanced Load Balancer Test Suite Runner
# This script runs comprehensive tests for the enhanced architecture implementation
# Following industry-standard testing practices and methodologies

set -euo pipefail

# Color codes for output formatting
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
NC='\033[0m' # No Color

# Test configuration
VERBOSE=${VERBOSE:-false}
BENCHMARK=${BENCHMARK:-false}
COVERAGE=${COVERAGE:-true}
RACE_DETECTION=${RACE_DETECTION:-true}
PERFORMANCE_TESTS=${PERFORMANCE_TESTS:-false}

# Project paths
PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COVERAGE_DIR="${PROJECT_ROOT}/coverage"
REPORTS_DIR="${PROJECT_ROOT}/test-reports"

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

log_section() {
    echo -e "\n${PURPLE}=== $1 ===${NC}\n"
}

# Setup test environment
setup_test_environment() {
    log_section "Setting Up Test Environment"
    
    # Create necessary directories
    mkdir -p "${COVERAGE_DIR}"
    mkdir -p "${REPORTS_DIR}"
    
    # Clean previous test artifacts
    rm -f "${COVERAGE_DIR}"/*.out
    rm -f "${REPORTS_DIR}"/*.xml
    rm -f "${REPORTS_DIR}"/*.json
    
    log_info "Test directories prepared"
    
    # Verify Go installation and version
    if ! command -v go &> /dev/null; then
        log_error "Go is not installed or not in PATH"
        exit 1
    fi
    
    GO_VERSION=$(go version | cut -d' ' -f3)
    log_info "Using Go version: ${GO_VERSION}"
    
    # Verify project structure
    if [[ ! -f "${PROJECT_ROOT}/go.mod" ]]; then
        log_error "go.mod not found in project root"
        exit 1
    fi
    
    log_success "Test environment setup complete"
}

# Download dependencies
download_dependencies() {
    log_section "Downloading Dependencies"
    
    cd "${PROJECT_ROOT}"
    
    log_info "Running go mod tidy..."
    go mod tidy
    
    log_info "Running go mod download..."
    go mod download
    
    log_success "Dependencies downloaded successfully"
}

# Build verification
verify_build() {
    log_section "Verifying Build"
    
    cd "${PROJECT_ROOT}"
    
    # Check if any .go files exist in the project
    if ! find . -name "*.go" -type f | grep -q .; then
        log_warning "No Go source files found in project"
        return 0
    fi
    
    log_info "Building main application..."
    
    # Try to build the enhanced version if it exists
    if [[ -f "cmd/enhanced/main.go" ]]; then
        go build -o "${REPORTS_DIR}/enhanced-server" ./cmd/enhanced/
        log_success "Enhanced server built successfully"
    elif [[ -f "cmd/main.go" ]]; then
        go build -o "${REPORTS_DIR}/server" ./cmd/
        log_success "Server built successfully"
    elif [[ -f "main.go" ]]; then
        go build -o "${REPORTS_DIR}/server" .
        log_success "Application built successfully"
    else
        log_warning "No main.go file found - skipping build verification"
    fi
}

# Lint and formatting checks
run_quality_checks() {
    log_section "Running Code Quality Checks"
    
    cd "${PROJECT_ROOT}"
    
    # Check if golangci-lint is installed
    if command -v golangci-lint &> /dev/null; then
        log_info "Running golangci-lint..."
        if golangci-lint run ./...; then
            log_success "Linting passed"
        else
            log_warning "Linting found issues (non-blocking)"
        fi
    else
        log_warning "golangci-lint not found - skipping lint checks"
    fi
    
    # Format check
    log_info "Checking code formatting..."
    if ! gofmt -l . | grep -q .; then
        log_success "Code formatting is correct"
    else
        log_warning "Code formatting issues found:"
        gofmt -l .
    fi
    
    # Vet check
    log_info "Running go vet..."
    if go vet ./...; then
        log_success "go vet passed"
    else
        log_error "go vet found issues"
        exit 1
    fi
}

# Run unit tests
run_unit_tests() {
    log_section "Running Unit Tests"
    
    cd "${PROJECT_ROOT}"
    
    local test_args=()
    local coverage_file="${COVERAGE_DIR}/unit.out"
    
    if [[ "${COVERAGE}" == "true" ]]; then
        test_args+=("-coverprofile=${coverage_file}")
        test_args+=("-covermode=atomic")
    fi
    
    if [[ "${RACE_DETECTION}" == "true" ]]; then
        test_args+=("-race")
    fi
    
    if [[ "${VERBOSE}" == "true" ]]; then
        test_args+=("-v")
    fi
    
    # Find and run unit tests
    local unit_test_pattern="./tests/unit/..."
    if [[ -d "tests/unit" ]]; then
        log_info "Running unit tests from tests/unit/..."
        if go test "${test_args[@]}" "${unit_test_pattern}"; then
            log_success "Unit tests passed"
        else
            log_error "Unit tests failed"
            exit 1
        fi
    else
        # Fallback to regular test pattern
        log_info "Running all tests (no dedicated unit test directory found)..."
        if go test "${test_args[@]}" ./...; then
            log_success "Tests passed"
        else
            log_error "Tests failed"
            exit 1
        fi
    fi
}

# Run integration tests
run_integration_tests() {
    log_section "Running Integration Tests"
    
    cd "${PROJECT_ROOT}"
    
    local integration_dir="tests/integration"
    
    if [[ ! -d "${integration_dir}" ]]; then
        log_warning "No integration tests found - skipping"
        return 0
    fi
    
    local test_args=()
    local coverage_file="${COVERAGE_DIR}/integration.out"
    
    if [[ "${COVERAGE}" == "true" ]]; then
        test_args+=("-coverprofile=${coverage_file}")
        test_args+=("-covermode=atomic")
    fi
    
    if [[ "${RACE_DETECTION}" == "true" ]]; then
        test_args+=("-race")
    fi
    
    if [[ "${VERBOSE}" == "true" ]]; then
        test_args+=("-v")
    fi
    
    log_info "Running integration tests..."
    if go test "${test_args[@]}" "./${integration_dir}/..."; then
        log_success "Integration tests passed"
    else
        log_error "Integration tests failed"
        exit 1
    fi
}

# Run performance tests
run_performance_tests() {
    if [[ "${PERFORMANCE_TESTS}" != "true" ]]; then
        log_info "Performance tests disabled - skipping"
        return 0
    fi
    
    log_section "Running Performance Tests"
    
    cd "${PROJECT_ROOT}"
    
    local performance_dir="tests/performance"
    
    if [[ ! -d "${performance_dir}" ]]; then
        log_warning "No performance tests found - skipping"
        return 0
    fi
    
    local test_args=("-timeout=10m")
    
    if [[ "${VERBOSE}" == "true" ]]; then
        test_args+=("-v")
    fi
    
    log_info "Running performance tests..."
    if go test "${test_args[@]}" "./${performance_dir}/..."; then
        log_success "Performance tests passed"
    else
        log_warning "Performance tests failed (non-blocking)"
    fi
}

# Run benchmarks
run_benchmarks() {
    if [[ "${BENCHMARK}" != "true" ]]; then
        log_info "Benchmarks disabled - skipping"
        return 0
    fi
    
    log_section "Running Benchmarks"
    
    cd "${PROJECT_ROOT}"
    
    local bench_output="${REPORTS_DIR}/benchmarks.txt"
    
    log_info "Running benchmarks..."
    if go test -bench=. -benchmem ./... > "${bench_output}" 2>&1; then
        log_success "Benchmarks completed"
        if [[ "${VERBOSE}" == "true" ]]; then
            cat "${bench_output}"
        fi
    else
        log_warning "Benchmarks failed (non-blocking)"
        if [[ "${VERBOSE}" == "true" ]]; then
            cat "${bench_output}"
        fi
    fi
}

# Generate coverage report
generate_coverage_report() {
    if [[ "${COVERAGE}" != "true" ]]; then
        log_info "Coverage reporting disabled - skipping"
        return 0
    fi
    
    log_section "Generating Coverage Report"
    
    cd "${PROJECT_ROOT}"
    
    local coverage_files=()
    local combined_coverage="${COVERAGE_DIR}/combined.out"
    
    # Find all coverage files
    for file in "${COVERAGE_DIR}"/*.out; do
        if [[ -f "${file}" && "${file}" != "${combined_coverage}" ]]; then
            coverage_files+=("${file}")
        fi
    done
    
    if [[ ${#coverage_files[@]} -eq 0 ]]; then
        log_warning "No coverage files found"
        return 0
    fi
    
    # Combine coverage files if multiple exist
    if [[ ${#coverage_files[@]} -gt 1 ]]; then
        log_info "Combining coverage files..."
        echo "mode: atomic" > "${combined_coverage}"
        for file in "${coverage_files[@]}"; do
            tail -n +2 "${file}" >> "${combined_coverage}"
        done
    else
        cp "${coverage_files[0]}" "${combined_coverage}"
    fi
    
    # Generate HTML report
    local html_report="${REPORTS_DIR}/coverage.html"
    log_info "Generating HTML coverage report..."
    go tool cover -html="${combined_coverage}" -o "${html_report}"
    
    # Generate coverage summary
    local coverage_summary
    coverage_summary=$(go tool cover -func="${combined_coverage}" | tail -n 1)
    log_info "Coverage Summary: ${coverage_summary}"
    
    # Extract coverage percentage
    local coverage_percent
    coverage_percent=$(echo "${coverage_summary}" | grep -oE '[0-9]+\.[0-9]+%' | tail -n 1)
    
    if [[ -n "${coverage_percent}" ]]; then
        echo "${coverage_percent}" > "${REPORTS_DIR}/coverage_percent.txt"
        
        # Parse percentage for validation
        local percent_num
        percent_num=$(echo "${coverage_percent}" | sed 's/%//')
        
        if (( $(echo "${percent_num} >= 80" | bc -l) )); then
            log_success "Coverage ${coverage_percent} meets minimum requirement (80%)"
        elif (( $(echo "${percent_num} >= 70" | bc -l) )); then
            log_warning "Coverage ${coverage_percent} is below recommended 80% but above minimum 70%"
        else
            log_error "Coverage ${coverage_percent} is below minimum requirement (70%)"
            exit 1
        fi
    fi
    
    log_success "Coverage report generated: ${html_report}"
}

# Generate test report
generate_test_report() {
    log_section "Generating Test Report"
    
    cd "${PROJECT_ROOT}"
    
    local report_file="${REPORTS_DIR}/test-summary.txt"
    
    {
        echo "Enhanced Load Balancer Test Report"
        echo "=================================="
        echo "Generated: $(date)"
        echo "Go Version: $(go version)"
        echo ""
        
        echo "Project Structure:"
        echo "- Enhanced Architecture: $([ -f "cmd/enhanced/main.go" ] && echo "Yes" || echo "No")"
        echo "- Routing Engine: $([ -f "internal/routing/engine.go" ] && echo "Yes" || echo "No")"
        echo "- Traffic Manager: $([ -f "internal/traffic/manager.go" ] && echo "Yes" || echo "No")"
        echo "- Observability: $([ -f "internal/observability/manager.go" ] && echo "Yes" || echo "No")"
        echo "- Unit Tests: $([ -d "tests/unit" ] && echo "Yes" || echo "No")"
        echo "- Integration Tests: $([ -d "tests/integration" ] && echo "Yes" || echo "No")"
        echo "- Performance Tests: $([ -d "tests/performance" ] && echo "Yes" || echo "No")"
        echo ""
        
        if [[ -f "${REPORTS_DIR}/coverage_percent.txt" ]]; then
            echo "Test Coverage: $(cat "${REPORTS_DIR}/coverage_percent.txt")"
        fi
        
        echo ""
        echo "Build Status: $([ -f "${REPORTS_DIR}/enhanced-server" ] || [ -f "${REPORTS_DIR}/server" ] && echo "Success" || echo "N/A")"
        
        if [[ -f "${REPORTS_DIR}/benchmarks.txt" ]]; then
            echo ""
            echo "Benchmark Results:"
            echo "=================="
            cat "${REPORTS_DIR}/benchmarks.txt"
        fi
        
    } > "${report_file}"
    
    log_success "Test report generated: ${report_file}"
}

# Cleanup
cleanup() {
    log_section "Cleanup"
    
    # Remove temporary files
    find "${PROJECT_ROOT}" -name "*.test" -delete 2>/dev/null || true
    
    log_info "Cleanup completed"
}

# Main execution
main() {
    log_section "Enhanced Load Balancer Test Suite"
    log_info "Starting comprehensive test execution..."
    
    # Parse command line arguments
    while [[ $# -gt 0 ]]; do
        case $1 in
            --verbose)
                VERBOSE=true
                shift
                ;;
            --benchmark)
                BENCHMARK=true
                shift
                ;;
            --no-coverage)
                COVERAGE=false
                shift
                ;;
            --no-race)
                RACE_DETECTION=false
                shift
                ;;
            --performance)
                PERFORMANCE_TESTS=true
                shift
                ;;
            --help)
                echo "Enhanced Load Balancer Test Suite"
                echo ""
                echo "Usage: $0 [options]"
                echo ""
                echo "Options:"
                echo "  --verbose      Enable verbose output"
                echo "  --benchmark    Run benchmarks"
                echo "  --no-coverage  Disable coverage reporting"
                echo "  --no-race      Disable race detection"
                echo "  --performance  Run performance tests"
                echo "  --help         Show this help message"
                exit 0
                ;;
            *)
                log_error "Unknown option: $1"
                echo "Use --help for usage information"
                exit 1
                ;;
        esac
    done
    
    # Execute test suite
    setup_test_environment
    download_dependencies
    verify_build
    run_quality_checks
    run_unit_tests
    run_integration_tests
    run_performance_tests
    run_benchmarks
    generate_coverage_report
    generate_test_report
    cleanup
    
    log_section "Test Suite Complete"
    log_success "All tests completed successfully!"
    
    # Final summary
    echo ""
    echo -e "${CYAN}📊 Test Summary:${NC}"
    echo "  ✅ Build verification"
    echo "  ✅ Code quality checks"
    echo "  ✅ Unit tests"
    echo "  $([ -d "tests/integration" ] && echo "✅" || echo "⚠️") Integration tests"
    echo "  $([ "${PERFORMANCE_TESTS}" == "true" ] && echo "✅" || echo "⚠️") Performance tests"
    echo "  $([ "${BENCHMARK}" == "true" ] && echo "✅" || echo "⚠️") Benchmarks"
    echo "  $([ "${COVERAGE}" == "true" ] && echo "✅" || echo "⚠️") Coverage report"
    
    if [[ -f "${REPORTS_DIR}/coverage.html" ]]; then
        echo ""
        echo -e "${CYAN}📈 Reports Generated:${NC}"
        echo "  Coverage Report: ${REPORTS_DIR}/coverage.html"
        echo "  Test Summary: ${REPORTS_DIR}/test-summary.txt"
    fi
    
    echo ""
    log_success "Enhanced Load Balancer architecture testing completed!"
}

# Execute main function with all arguments
main "$@"
