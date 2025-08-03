package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	"github.com/mir00r/load-balancer/internal/config"
	"github.com/mir00r/load-balancer/internal/domain"
	"github.com/mir00r/load-balancer/internal/handler"
	"github.com/mir00r/load-balancer/internal/middleware"
	"github.com/mir00r/load-balancer/internal/repository"
	"github.com/mir00r/load-balancer/internal/service"
	"github.com/mir00r/load-balancer/pkg/logger"
)

func main() {
	// Load configuration
	cfg := config.DefaultConfig()

	// Initialize logger
	loggerConfig := logger.Config{
		Level:      cfg.Logging.Level,
		Format:     cfg.Logging.Format,
		Output:     cfg.Logging.Output,
		File:       cfg.Logging.File,
		MaxSize:    cfg.Logging.MaxSize,
		MaxBackups: cfg.Logging.MaxBackups,
		MaxAge:     cfg.Logging.MaxAge,
		Compress:   cfg.Logging.Compress,
	}
	logger, err := logger.New(loggerConfig)
	if err != nil {
		log.Fatalf("Failed to create logger: %v", err)
	}

	// Create repositories
	backendRepo := repository.NewInMemoryBackendRepository()

	// Add default backends from config
	for _, backendCfg := range cfg.Backends {
		backend := &domain.Backend{
			ID:              backendCfg.ID,
			URL:             backendCfg.URL,
			Weight:          backendCfg.Weight,
			HealthCheckPath: backendCfg.HealthCheckPath,
			MaxConnections:  backendCfg.MaxConnections,
			Timeout:         backendCfg.Timeout,
		}
		backend.SetStatus(domain.StatusHealthy)
		if err := backendRepo.Save(backend); err != nil {
			log.Fatalf("Failed to add backend %s: %v", backend.ID, err)
		}
	}

	// Create metrics service
	metrics := service.NewMetrics()

	// Create health checker
	healthChecker := service.NewHealthChecker(cfg.LoadBalancer.HealthCheck, logger)

	// Create load balancer
	loadBalancer, err := service.NewLoadBalancer(
		domain.LoadBalancerConfig{
			Strategy:   domain.LoadBalancingStrategy(cfg.LoadBalancer.Strategy),
			Port:       cfg.LoadBalancer.Port,
			MaxRetries: cfg.LoadBalancer.MaxRetries,
			RetryDelay: cfg.LoadBalancer.RetryDelay,
			Timeout:    cfg.LoadBalancer.Timeout,
		},
		backendRepo,
		healthChecker,
		metrics,
		logger,
	)
	if err != nil {
		log.Fatalf("Failed to create load balancer: %v", err)
	}

	// Set up load balancing strategy
	if err := loadBalancer.SetStrategy(domain.LoadBalancingStrategy(cfg.LoadBalancer.Strategy)); err != nil {
		log.Fatalf("Failed to set load balancing strategy: %v", err)
	}

	// Start health checking (if it has a Start method)
	// healthChecker.Start(context.Background(), loadBalancer)

	// Create handlers
	lbHandler := handler.NewLoadBalancerHandler(
		loadBalancer,
		metrics,
		logger,
		domain.LoadBalancerConfig{
			Strategy:   domain.LoadBalancingStrategy(cfg.LoadBalancer.Strategy),
			Port:       cfg.LoadBalancer.Port,
			MaxRetries: cfg.LoadBalancer.MaxRetries,
			RetryDelay: cfg.LoadBalancer.RetryDelay,
			Timeout:    cfg.LoadBalancer.Timeout,
		},
	)

	// Create Prometheus handler if metrics are enabled
	var prometheusHandler *handler.PrometheusHandler
	if cfg.Metrics.Enabled {
		prometheusHandler = handler.NewPrometheusHandler(loadBalancer, metrics, logger)
	}

	// Create security middleware if configured
	var securityMiddleware *middleware.SecurityMiddleware
	if cfg.Security != nil {
		securityMiddleware, err = middleware.NewSecurityMiddleware(*cfg.Security, logger)
		if err != nil {
			log.Fatalf("Failed to create security middleware: %v", err)
		}
	}

	// Create router
	router := mux.NewRouter()

	// Setup routes
	router.HandleFunc("/health", healthCheckHandler(loadBalancer)).Methods("GET")

	// Metrics endpoint
	if prometheusHandler != nil {
		router.HandleFunc("/metrics", prometheusHandler.MetricsHandler).Methods("GET")
		router.HandleFunc("/metrics/health", prometheusHandler.HealthCheckMetricsHandler).Methods("GET")
		router.HandleFunc("/metrics/config", prometheusHandler.ScrapeConfigHandler).Methods("GET")
	}

	// Load balancer proxy (catch-all)
	router.PathPrefix("/").Handler(lbHandler)

	// Apply middleware
	var handler http.Handler = router

	// Apply security middleware if configured
	if securityMiddleware != nil {
		handler = securityMiddleware.SecurityMiddleware()(handler)
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.LoadBalancer.Port),
		Handler:      handler,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Start server in a goroutine
	go func() {
		logger.WithField("port", cfg.LoadBalancer.Port).Info("Starting load balancer server")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Wait for interrupt signal to gracefully shutdown the server
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("Shutting down server...")

	// Create a context with timeout for graceful shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown server
	if err := srv.Shutdown(ctx); err != nil {
		log.Fatalf("Server forced to shutdown: %v", err)
	}

	logger.Info("Server exited")
}

// healthCheckHandler provides a simple health check endpoint
func healthCheckHandler(lb domain.LoadBalancer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		healthyBackends := lb.GetHealthyBackends()
		allBackends := lb.GetBackends()

		status := map[string]interface{}{
			"status":           "healthy",
			"total_backends":   len(allBackends),
			"healthy_backends": len(healthyBackends),
			"timestamp":        time.Now().UTC().Format(time.RFC3339),
		}

		w.Header().Set("Content-Type", "application/json")
		if len(healthyBackends) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			status["status"] = "unhealthy"
		} else {
			w.WriteHeader(http.StatusOK)
		}

		// Simple JSON response
		fmt.Fprintf(w, `{"status":"%s","total_backends":%d,"healthy_backends":%d,"timestamp":"%s"}`,
			status["status"], status["total_backends"], status["healthy_backends"], status["timestamp"])
	}
}
