package main

import (
	"context"
	"encoding/json"
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
	httpSwagger "github.com/swaggo/http-swagger"
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

	// Create admin handler
	adminHandler := handler.NewAdminHandler(loadBalancer, metrics, logger)

	// Create configuration handler
	configHandler := handler.NewConfigHandler(loadBalancer, logger)

	// Create additional NGINX-style handlers
	staticHandler := handler.NewStaticHandler("./static", logger) // Local static content directory
	proxyPassHandler := handler.NewProxyPassHandler(logger)
	wsHandler := handler.NewWebSocketProxyHandler(loadBalancer, metrics, logger)
	// tcpHandler := handler.NewTCPProxyHandler(loadBalancer, metrics, logger) // For Layer 4 TCP proxying - requires separate TCP server

	// Create NGINX-style middleware
	var rateLimitMiddleware *middleware.RateLimitMiddleware
	if cfg.RateLimit != nil && cfg.RateLimit.Enabled {
		// Convert config types to middleware types
		rateLimitConfig := middleware.RateLimitConfig{
			Enabled:         cfg.RateLimit.Enabled,
			RequestsPerSec:  cfg.RateLimit.RequestsPerSec,
			BurstSize:       cfg.RateLimit.BurstSize,
			CleanupInterval: cfg.RateLimit.CleanupInterval,
			MaxClients:      cfg.RateLimit.MaxClients,
			WhitelistedIPs:  cfg.RateLimit.WhitelistedIPs,
			BlacklistedIPs:  cfg.RateLimit.BlacklistedIPs,
			PathRules:       make(map[string]middleware.PathRateLimit),
		}
		// Convert path rules
		for path, rule := range cfg.RateLimit.PathRules {
			rateLimitConfig.PathRules[path] = middleware.PathRateLimit{
				RequestsPerSec: rule.RequestsPerSec,
				BurstSize:      rule.BurstSize,
			}
		}
		rateLimitMiddleware = middleware.NewRateLimitMiddleware(rateLimitConfig, logger)
	}

	var authMiddleware *middleware.AuthMiddleware
	if cfg.Auth != nil && cfg.Auth.Enabled {
		// Convert config types to middleware types
		authConfig := middleware.AuthConfig{
			Enabled:   cfg.Auth.Enabled,
			Type:      cfg.Auth.Type,
			Users:     cfg.Auth.Users,
			Realm:     cfg.Auth.Realm,
			PathRules: make(map[string]middleware.PathAuth),
		}
		// Convert path rules
		for path, rule := range cfg.Auth.PathRules {
			authConfig.PathRules[path] = middleware.PathAuth{
				Required: rule.Required,
				Users:    rule.Users,
				Methods:  rule.Methods,
			}
		}
		authMiddleware = middleware.NewAuthMiddleware(authConfig, logger)
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

	// Serve swagger.yaml file first (most specific)
	router.HandleFunc("/docs/swagger.yaml", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "./docs/swagger.yaml")
	}).Methods("GET")

	// Swagger documentation endpoint (specific prefix)
	docsRouter := router.PathPrefix("/docs/").Subrouter()
	docsRouter.PathPrefix("/").Handler(httpSwagger.Handler(
		httpSwagger.URL("swagger.yaml"), // URL to the swagger.yaml file
		httpSwagger.DeepLinking(true),
		httpSwagger.DocExpansion("list"),
		httpSwagger.DomID("swagger-ui"),
	))

	// Health endpoints (specific routes)
	router.HandleFunc("/health", healthCheckHandler(loadBalancer)).Methods("GET")

	// Metrics endpoints (specific routes)
	if prometheusHandler != nil {
		router.HandleFunc("/metrics", prometheusHandler.MetricsHandler).Methods("GET")
		router.HandleFunc("/metrics/health", prometheusHandler.HealthCheckMetricsHandler).Methods("GET")
		router.HandleFunc("/metrics/config", prometheusHandler.ScrapeConfigHandler).Methods("GET")
	}

	// API endpoints
	apiRouter := router.PathPrefix("/api/v1").Subrouter()
	apiRouter.HandleFunc("/health", enhancedHealthCheckHandler(loadBalancer, metrics)).Methods("GET")

	// Admin API endpoints
	if cfg.Admin.Enabled {
		adminRouter := apiRouter.PathPrefix("/admin").Subrouter()

		// Backend management
		adminRouter.HandleFunc("/backends", adminHandler.ListBackendsHandler).Methods("GET")
		adminRouter.HandleFunc("/backends", adminHandler.AddBackendHandler).Methods("POST")
		adminRouter.HandleFunc("/backends/{id}", adminHandler.DeleteBackendHandler).Methods("DELETE")

		// Configuration and stats
		adminRouter.HandleFunc("/config", adminHandler.GetConfigHandler).Methods("GET")
		adminRouter.HandleFunc("/stats", adminHandler.GetStatsHandler).Methods("GET")

		// Configuration reload endpoint
		adminRouter.HandleFunc("/reload", configHandler.ReloadConfigHandler).Methods("POST")
		adminRouter.HandleFunc("/config/current", configHandler.GetConfigHandler).Methods("GET")
	}

	// NGINX-style feature endpoints (specific prefixes)
	// Static content serving
	router.PathPrefix("/static/").Handler(http.StripPrefix("/static/", staticHandler))

	// WebSocket proxy for /ws/ prefix
	router.PathPrefix("/ws/").Handler(wsHandler)

	// Proxy pass for specific routes (example configuration)
	proxyPassHandler.AddRoute("/api/", "http://api.backend.com", "/api")
	router.PathPrefix("/proxy/").Handler(proxyPassHandler)

	// Load balancer proxy (catch-all, MUST be last)
	router.PathPrefix("/").Handler(lbHandler)

	// Apply middleware chain
	var handlerChain http.Handler = router

	// Apply rate limiting middleware if configured
	if rateLimitMiddleware != nil {
		handlerChain = rateLimitMiddleware.RateLimit()(handlerChain)
		logger.Info("Rate limiting middleware enabled")
	}

	// Apply authentication middleware if configured
	if authMiddleware != nil {
		if cfg.Auth.Type == "basic" {
			handlerChain = authMiddleware.BasicAuth()(handlerChain)
		} else if cfg.Auth.Type == "bearer" {
			handlerChain = authMiddleware.BearerAuth()(handlerChain)
		}
		logger.WithFields(map[string]interface{}{
			"auth_type": cfg.Auth.Type,
		}).Info("Authentication middleware enabled")
	}

	// Apply security middleware if configured
	if securityMiddleware != nil {
		handlerChain = securityMiddleware.SecurityMiddleware()(handlerChain)
		logger.Info("Security middleware enabled")
	}

	// Create HTTP server
	srv := &http.Server{
		Addr:         fmt.Sprintf(":%d", cfg.LoadBalancer.Port),
		Handler:      handlerChain,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  cfg.Server.IdleTimeout,
	}

	// Start server in a goroutine
	go func() {
		logger.WithFields(map[string]interface{}{
			"port": cfg.LoadBalancer.Port,
			"docs": fmt.Sprintf("http://localhost:%d/docs/", cfg.LoadBalancer.Port),
		}).Info("Starting production load balancer server")

		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	// Print startup information
	fmt.Printf("\n🚀 Load Balancer Server Started Successfully!\n")
	fmt.Printf("📊 Server URL: http://localhost:%d\n", cfg.LoadBalancer.Port)
	fmt.Printf("📖 API Documentation: http://localhost:%d/docs/\n", cfg.LoadBalancer.Port)
	fmt.Printf("❤️  Health Check: http://localhost:%d/health\n", cfg.LoadBalancer.Port)
	fmt.Printf("📈 Metrics: http://localhost:%d/metrics\n", cfg.LoadBalancer.Port)
	if cfg.Admin.Enabled {
		fmt.Printf("⚙️  Admin API: http://localhost:%d/api/v1/admin/\n", cfg.LoadBalancer.Port)
	}
	fmt.Printf("🛑 Press Ctrl+C to stop\n\n")

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

		w.Header().Set("Content-Type", "application/json")
		if len(healthyBackends) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		// Simple JSON response
		fmt.Fprintf(w, `{"status":"%s","total_backends":%d,"healthy_backends":%d,"timestamp":"%s"}`,
			getStatusString(len(healthyBackends) > 0), len(allBackends), len(healthyBackends), time.Now().UTC().Format(time.RFC3339))
	}
}

// enhancedHealthCheckHandler provides detailed health information
func enhancedHealthCheckHandler(lb domain.LoadBalancer, metrics domain.Metrics) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		healthyBackends := lb.GetHealthyBackends()
		allBackends := lb.GetBackends()

		// Get overall stats
		overallStats := metrics.GetStats()
		var totalRequests, totalErrors int64
		if req, exists := overallStats["total_requests"]; exists {
			if reqInt, ok := req.(int64); ok {
				totalRequests = reqInt
			}
		}
		if err, exists := overallStats["total_errors"]; exists {
			if errInt, ok := err.(int64); ok {
				totalErrors = errInt
			}
		}

		// Calculate success rate
		successRate := 100.0
		if totalRequests > 0 {
			successRate = float64(totalRequests-totalErrors) / float64(totalRequests) * 100
		}

		// Backend details
		var backendDetails []map[string]interface{}
		for _, backend := range allBackends {
			isHealthy := false
			for _, hb := range healthyBackends {
				if hb.ID == backend.ID {
					isHealthy = true
					break
				}
			}

			stats := metrics.GetBackendStats(backend.ID)
			var backendRequests int64
			if req, exists := stats["requests"]; exists {
				if reqInt, ok := req.(int64); ok {
					backendRequests = reqInt
				}
			}

			backendDetails = append(backendDetails, map[string]interface{}{
				"id":                 backend.ID,
				"url":                backend.URL,
				"status":             getStatusString(isHealthy),
				"active_connections": backend.GetActiveConnections(),
				"total_requests":     backendRequests,
			})
		}

		response := map[string]interface{}{
			"status":           getStatusString(len(healthyBackends) > 0),
			"total_backends":   len(allBackends),
			"healthy_backends": len(healthyBackends),
			"success_rate":     successRate,
			"total_requests":   totalRequests,
			"total_errors":     totalErrors,
			"timestamp":        time.Now().UTC().Format(time.RFC3339),
			"backends":         backendDetails,
		}

		w.Header().Set("Content-Type", "application/json")
		if len(healthyBackends) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
		} else {
			w.WriteHeader(http.StatusOK)
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			http.Error(w, "Failed to encode response", http.StatusInternalServerError)
		}
	}
}

func getStatusString(isHealthy bool) string {
	if isHealthy {
		return "healthy"
	}
	return "unhealthy"
}
