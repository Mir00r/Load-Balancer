package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/internal/handler"
	"github.com/mir00r/load-balancer/internal/middleware"
	"github.com/mir00r/load-balancer/internal/repository"
	"github.com/mir00r/load-balancer/internal/service"
	"github.com/mir00r/load-balancer/pkg/logger"
)

const (
	shutdownTimeout = 30 * time.Second
)

// getConfigSource returns the configuration source for logging
func getConfigSource() string {
	if configFile := os.Getenv("CONFIG_FILE"); configFile != "" {
		if _, err := os.Stat(configFile); err == nil {
			return "file+env"
		}
	}

	// Check if any environment variables are set
	envVars := []string{
		"LB_PORT", "LB_STRATEGY", "LB_BACKENDS", "LB_LOG_LEVEL",
		"LB_HEALTH_CHECK_ENABLED", "LB_RATE_LIMIT_ENABLED",
	}

	for _, envVar := range envVars {
		if os.Getenv(envVar) != "" {
			return "environment"
		}
	}

	return "defaults"
}

func main() {
	// Factor #12: Admin Processes - Run admin/management tasks as one-off processes
	if checkIfAdminMode() {
		runAdminProcess()
		return
	}

	// Load configuration following 12-Factor App methodology
	// Factor #3: Config - Store config in the environment
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Printf("Failed to load configuration: %v\n", err)
		os.Exit(1)
	}

	// Initialize logger
	log, err := logger.New(logger.Config{
		Level:      cfg.Logging.Level,
		Format:     cfg.Logging.Format,
		Output:     cfg.Logging.Output,
		File:       cfg.Logging.File,
		MaxSize:    cfg.Logging.MaxSize,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAge:     cfg.Logging.MaxAge,
		Compress:   cfg.Logging.Compress,
	})
	if err != nil {
		fmt.Printf("Failed to initialize logger: %v\n", err)
		os.Exit(1)
	}

	log.Info("Starting Load Balancer")

	// Add configured backends
	backends := cfg.ToBackends()

	log.WithFields(map[string]interface{}{
		"version":       "1.0.0",
		"strategy":      cfg.LoadBalancer.Strategy,
		"port":          cfg.LoadBalancer.Port,
		"backends":      len(backends),
		"config_source": getConfigSource(),
		"process":       getProcessInfo(),
	}).Info("Load balancer configuration loaded")

	// Initialize components
	backendRepo := repository.NewInMemoryBackendRepository()

	// Save backends to repository
	if err := backendRepo.SaveAll(backends); err != nil {
		log.WithError(err).Fatal("Failed to save backends")
	}

	log.Infof("Loaded %d backends", len(backends))

	// Initialize health checker
	healthChecker := service.NewHealthChecker(cfg.LoadBalancer.HealthCheck, log)

	// Initialize metrics
	metrics := service.NewMetrics()

	// Initialize load balancer
	lbConfig := cfg.ToLoadBalancerConfig()
	loadBalancer, err := service.NewLoadBalancer(lbConfig, backendRepo, healthChecker, metrics, log)
	if err != nil {
		log.WithError(err).Fatal("Failed to create load balancer")
	}

	// Initialize HTTP handler
	lbHandler := handler.NewLoadBalancerHandler(loadBalancer, metrics, log, lbConfig)

	// Initialize health handler for Kubernetes/Docker health checks
	// Factor #9: Disposability - Fast startup and graceful shutdown
	healthHandler := handler.NewHealthHandler("1.0.0")

	// Setup middleware
	var middlewares []func(http.Handler) http.Handler

	// Add common middleware
	middlewares = append(middlewares,
		middleware.RecoveryMiddleware(log),
		middleware.LoggingMiddleware(log),
		middleware.CORSMiddleware(),
		middleware.SecurityHeadersMiddleware(),
		middleware.TimeoutMiddleware(lbConfig.Timeout, log),
	)

	// Add rate limiting if enabled
	if lbConfig.RateLimit.Enabled {
		rateLimiter := middleware.NewRateLimiter(lbConfig.RateLimit, log)
		middlewares = append(middlewares, rateLimiter.RateLimitMiddleware())
		log.Info("Rate limiting enabled")
	}

	// Add circuit breaker if enabled
	if lbConfig.CircuitBreaker.Enabled {
		circuitBreaker := middleware.NewCircuitBreaker(lbConfig.CircuitBreaker, log)
		middlewares = append(middlewares, middleware.CircuitBreakerMiddleware(circuitBreaker))
		log.Info("Circuit breaker enabled")
	}

	// Create HTTP server
	mux := http.NewServeMux()

	// Register routes
	mux.HandleFunc("/health", lbHandler.HealthHandler)
	mux.HandleFunc("/readiness", healthHandler.ReadinessHandler) // Kubernetes readiness probe
	mux.HandleFunc("/liveness", healthHandler.LivenessHandler)   // Kubernetes liveness probe
	mux.HandleFunc("/metrics", lbHandler.MetricsHandler)
	mux.HandleFunc("/status", lbHandler.StatusHandler)
	mux.HandleFunc("/backends", lbHandler.BackendsHandler)
	mux.HandleFunc("/backends/", lbHandler.BackendsHandler)

	// Default handler for load balancing
	mux.Handle("/", lbHandler)

	// Apply middleware
	var finalHandler http.Handler = mux
	for i := len(middlewares) - 1; i >= 0; i-- {
		finalHandler = middlewares[i](finalHandler)
	}

	// Factor #7: Port Binding - Export services via port binding
	// Allow PORT environment variable to override config (for cloud platforms)
	port := getPort(lbConfig.Port)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      finalHandler,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Start load balancer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := loadBalancer.Start(ctx); err != nil {
		log.WithError(err).Fatal("Failed to start load balancer")
	}

	// Start HTTP server
	go func() {
		log.WithFields(map[string]interface{}{
			"port":     port,
			"strategy": lbConfig.Strategy,
			"backends": len(backends),
		}).Info("Starting HTTP server")

		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Fatal("HTTP server failed")
		}
	}()

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Info("Shutdown signal received")

	// Graceful shutdown
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer shutdownCancel()

	// Stop load balancer
	if err := loadBalancer.Stop(shutdownCtx); err != nil {
		log.WithError(err).Error("Error stopping load balancer")
	}

	// Stop HTTP server
	if err := server.Shutdown(shutdownCtx); err != nil {
		log.WithError(err).Error("Error shutting down HTTP server")
	}

	log.Info("Load balancer stopped gracefully")
}
