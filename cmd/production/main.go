package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/handler"
	"github.com/mir00r/load-balancer/internal/middleware"
	"github.com/mir00r/load-balancer/internal/repository"
	"github.com/mir00r/load-balancer/internal/service"
	"github.com/mir00r/load-balancer/pkg/logger"
)

const shutdownTimeout = 30 * time.Second

func main() {
	// Initialize logger with config
	loggerConfig := logger.Config{
		Level:  "info",
		Format: "json",
		Output: "stdout",
	}
	log, err := logger.New(loggerConfig)
	if err != nil {
		panic(fmt.Sprintf("Failed to create logger: %v", err))
	}

	// Parse command line arguments for port
	port := 8080
	if len(os.Args) > 1 {
		if p, err := strconv.Atoi(os.Args[1]); err == nil {
			port = p
		}
	}

	// Factor #11: Logs - Treat logs as event streams
	log.WithFields(map[string]interface{}{
		"port":          port,
		"config_source": getConfigSource(),
		"version":       "production-v1.0.0",
		"build_time":    time.Now().Format(time.RFC3339),
	}).Info("Starting production-grade load balancer")

	// Factor #3: Config - Store config in the environment
	cfg, err := config.LoadConfig()
	if err != nil {
		log.WithError(err).Fatal("Failed to load configuration")
	}

	// Override port from command line
	cfg.LoadBalancer.Port = port

	// Factor #9: Disposability - Fast startup and graceful shutdown
	// Create repositories
	backendRepo := repository.NewInMemoryBackendRepository()

	// Initialize metrics
	metrics := service.NewMetrics()

	// Create health checker
	healthChecker := service.NewHealthChecker(cfg.LoadBalancer.HealthCheck, log)

	// Convert config to domain config
	domainConfig := domain.LoadBalancerConfig{
		Strategy:       domain.LoadBalancingStrategy(cfg.LoadBalancer.Strategy),
		RateLimit:      cfg.LoadBalancer.RateLimit,
		CircuitBreaker: cfg.LoadBalancer.CircuitBreaker,
	}

	// Create load balancer service
	loadBalancer, err := service.NewLoadBalancer(domainConfig, backendRepo, healthChecker, metrics, log)
	if err != nil {
		log.WithError(err).Fatal("Failed to create load balancer service")
	}

	// Set load balancing strategy
	if err := loadBalancer.SetStrategy(domain.LoadBalancingStrategy(cfg.LoadBalancer.Strategy)); err != nil {
		log.WithError(err).Fatal("Failed to set load balancing strategy")
	}

	// Add backends from configuration
	for _, backendCfg := range cfg.Backends {
		backend := &domain.Backend{
			ID:              backendCfg.ID,
			URL:             backendCfg.URL,
			Weight:          backendCfg.Weight,
			HealthCheckPath: backendCfg.HealthCheckPath,
			MaxConnections:  backendCfg.MaxConnections,
			Timeout:         backendCfg.Timeout,
		}
		if err := loadBalancer.AddBackend(backend); err != nil {
			log.WithError(err).WithField("backend", backend.ID).Error("Failed to add backend")
		}
	}

	// Factor #8: Concurrency - Scale out via the process model
	// Start load balancer with health checking
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := loadBalancer.Start(ctx); err != nil {
		log.WithError(err).Fatal("Failed to start load balancer")
	}

	// Create middleware stack
	var middlewareStack []func(http.Handler) http.Handler

	// Add security middleware if configured
	if cfg.Security != nil {
		securityMW, err := middleware.NewSecurityMiddleware(*cfg.Security, log)
		if err != nil {
			log.WithError(err).Warn("Failed to create security middleware")
		} else {
			middlewareStack = append(middlewareStack, securityMW.SecurityMiddleware())
		}
	}

	// Add common middleware
	middlewareStack = append(middlewareStack,
		middleware.RecoveryMiddleware(log),
		middleware.LoggingMiddleware(log),
		middleware.CORSMiddleware(),
	)

	// Create reverse proxy handler
	proxyHandler := handler.NewLoadBalancerHandler(
		loadBalancer,
		metrics,
		log,
		domain.LoadBalancerConfig{
			Strategy:   domain.LoadBalancingStrategy(cfg.LoadBalancer.Strategy),
			Port:       cfg.LoadBalancer.Port,
			MaxRetries: cfg.LoadBalancer.MaxRetries,
			RetryDelay: cfg.LoadBalancer.RetryDelay,
			Timeout:    cfg.LoadBalancer.Timeout,
		},
	)

	// Create HTTP server with middleware stack
	var finalHandler http.Handler = proxyHandler
	for i := len(middlewareStack) - 1; i >= 0; i-- {
		finalHandler = middlewareStack[i](finalHandler)
	}

	// Add health endpoint
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		backends := loadBalancer.GetHealthyBackends()
		status := "healthy"
		statusCode := http.StatusOK

		if len(backends) == 0 {
			status = "unhealthy"
			statusCode = http.StatusServiceUnavailable
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		fmt.Fprintf(w, `{
			"status": "%s",
			"healthy_backends": %d,
			"total_backends": %d,
			"timestamp": "%s",
			"version": "production-v1.0.0"
		}`, status, len(backends), len(loadBalancer.GetBackends()), time.Now().Format(time.RFC3339))
	})

	// Add metrics endpoint if enabled
	if cfg.Metrics.Enabled {
		prometheusHandler := handler.NewPrometheusHandler(loadBalancer, metrics, log)
		mux.HandleFunc("/metrics", prometheusHandler.MetricsHandler)
	}

	// Route everything else to load balancer
	mux.Handle("/", finalHandler)

	// Factor #7: Port binding - Export services via port binding
	server := &http.Server{
		Addr:         fmt.Sprintf(":%d", port),
		Handler:      mux,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Factor #5: Build, release, run - Strictly separate build and run stages
	log.WithFields(map[string]interface{}{
		"port":             port,
		"strategy":         cfg.LoadBalancer.Strategy,
		"backends":         len(cfg.Backends),
		"healthy_backends": len(loadBalancer.GetHealthyBackends()),
		"metrics_enabled":  cfg.Metrics.Enabled,
		"security_enabled": cfg.Security != nil,
		"features": []string{
			"IP-Hash-Load-Balancing",
			"Health-Checks",
			"Rate-Limiting",
			"Circuit-Breaker",
			"Prometheus-Metrics",
			"Security-Middleware",
			"TLS-Termination",
		},
	}).Info("Production-grade load balancer started successfully")

	// Start HTTP server
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.WithError(err).Fatal("HTTP server failed")
		}
	}()

	// Factor #9: Disposability - Maximize robustness with fast startup and graceful shutdown
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

	log.Info("Production-grade load balancer stopped gracefully")
}

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
