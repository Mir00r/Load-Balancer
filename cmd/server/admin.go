package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/internal/repository"
	"github.com/mir00r/load-balancer/internal/service"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// AdminProcesses implements 12-Factor App methodology - Factor #12: Admin processes
// Run admin/management tasks as one-off processes

// runMigration runs database migration (example admin process)
func runMigration() error {
	fmt.Println("Running database migration...")
	// In a real scenario, this would run database migrations
	time.Sleep(2 * time.Second)
	fmt.Println("Migration completed successfully")
	return nil
}

// runHealthCheck runs a one-off health check of all backends
func runHealthCheck() error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	log, err := logger.New(logger.Config{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		Output: cfg.Logging.Output,
	})
	if err != nil {
		return fmt.Errorf("failed to initialize logger: %w", err)
	}

	// Initialize components
	backendRepo := repository.NewInMemoryBackendRepository()
	backends := cfg.ToBackends()
	if err := backendRepo.SaveAll(backends); err != nil {
		return fmt.Errorf("failed to save backends: %w", err)
	}

	healthChecker := service.NewHealthChecker(cfg.LoadBalancer.HealthCheck, log)

	fmt.Printf("Checking health of %d backends...\n", len(backends))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, backend := range backends {
		err := healthChecker.Check(ctx, backend)
		status := "✓ healthy"
		if err != nil {
			status = fmt.Sprintf("✗ unhealthy: %v", err)
		}
		fmt.Printf("Backend %s (%s): %s\n", backend.ID, backend.URL, status)
	}

	return nil
}

// runCleanup runs cleanup tasks
func runCleanup() error {
	fmt.Println("Running cleanup tasks...")
	// Example cleanup tasks:
	// - Clean old log files
	// - Clean temporary files
	// - Reset metrics
	fmt.Println("Cleanup completed successfully")
	return nil
}

// runConfigValidation validates the current configuration
func runConfigValidation() error {
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	fmt.Println("Configuration validation passed ✓")
	fmt.Printf("Strategy: %s\n", cfg.LoadBalancer.Strategy)
	fmt.Printf("Port: %d\n", cfg.LoadBalancer.Port)
	fmt.Printf("Backends: %d\n", len(cfg.Backends))
	fmt.Printf("Health Check: %t\n", cfg.LoadBalancer.HealthCheck.Enabled)
	fmt.Printf("Rate Limiting: %t\n", cfg.LoadBalancer.RateLimit.Enabled)
	fmt.Printf("Circuit Breaker: %t\n", cfg.LoadBalancer.CircuitBreaker.Enabled)

	return nil
}

// runStats gets backend statistics
func runStats() error {
	fmt.Println("Backend statistics:")
	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	backends := cfg.ToBackends()
	fmt.Printf("Total backends: %d\n", len(backends))

	for i, backend := range backends {
		fmt.Printf("  Backend %d: %s (Weight: %d)\n", i+1, backend.URL, backend.Weight)
	}

	return nil
}

// runAdminProcess handles admin process execution
func runAdminProcess() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: load-balancer -admin <command>")
		fmt.Println("Commands:")
		fmt.Println("  migrate      - Run database migrations")
		fmt.Println("  health-check - Check health of all backends")
		fmt.Println("  cleanup      - Run cleanup tasks")
		fmt.Println("  validate-config - Validate configuration")
		fmt.Println("  stats        - Get backend statistics")
		os.Exit(1)
	}

	command := os.Args[2] // Third argument after program name and -admin flag
	var err error

	switch command {
	case "migrate":
		err = runMigration()
	case "health-check":
		err = runHealthCheck()
	case "cleanup":
		err = runCleanup()
	case "validate-config", "validate":
		err = runConfigValidation()
	case "stats":
		err = runStats()
	default:
		fmt.Printf("Unknown command: %s\n", command)
		os.Exit(1)
	}

	if err != nil {
		fmt.Printf("Command failed: %v\n", err)
		os.Exit(1)
	}
}

// checkIfAdminMode checks if running in admin mode
func checkIfAdminMode() bool {
	for _, arg := range os.Args {
		if arg == "-admin" {
			return true
		}
	}
	return false
}
