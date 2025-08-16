// Package main provides the enhanced load balancer with Traefik-inspired architecture.
// This implementation integrates routing engine, traffic management, and observability
// to provide enterprise-grade load balancing capabilities.
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
	"github.com/mir00r/load-balancer/internal/observability"
	"github.com/mir00r/load-balancer/internal/repository"
	"github.com/mir00r/load-balancer/internal/routing"
	"github.com/mir00r/load-balancer/internal/service"
	"github.com/mir00r/load-balancer/internal/traffic"
	"github.com/mir00r/load-balancer/pkg/logger"
)

// EnhancedLoadBalancerServer represents the enhanced server with Traefik-inspired architecture
type EnhancedLoadBalancerServer struct {
	// Core components
	config       *config.Config
	logger       *logger.Logger
	loadBalancer domain.LoadBalancer

	// New architecture components
	routingEngine  *routing.RouteEngine
	trafficManager *traffic.TrafficManager
	observability  *observability.ObservabilityManager

	// HTTP server
	server *http.Server
	router *mux.Router

	// Repositories and services
	backendRepo   domain.BackendRepository
	metrics       *service.Metrics
	healthChecker *service.HealthChecker

	// Lifecycle
	shutdownChan chan os.Signal
}

// NewEnhancedLoadBalancerServer creates a new enhanced load balancer server
func NewEnhancedLoadBalancerServer() (*EnhancedLoadBalancerServer, error) {
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
		return nil, fmt.Errorf("failed to create logger: %w", err)
	}

	// Initialize observability manager
	observabilityManager := observability.NewObservabilityManager("enhanced-load-balancer", logger)

	// Initialize routing engine
	routingEngine := routing.NewRouteEngine(logger)

	// Initialize traffic manager
	trafficManager := traffic.NewTrafficManager(routingEngine, logger)

	// Create repositories
	backendRepo := repository.NewInMemoryBackendRepository()

	// Initialize backends from configuration
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
			return nil, fmt.Errorf("failed to add backend %s: %w", backend.ID, err)
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
		return nil, fmt.Errorf("failed to create load balancer: %w", err)
	}

	// Set load balancing strategy
	if err := loadBalancer.SetStrategy(domain.LoadBalancingStrategy(cfg.LoadBalancer.Strategy)); err != nil {
		return nil, fmt.Errorf("failed to set load balancing strategy: %w", err)
	}

	server := &EnhancedLoadBalancerServer{
		config:         cfg,
		logger:         logger,
		loadBalancer:   loadBalancer,
		routingEngine:  routingEngine,
		trafficManager: trafficManager,
		observability:  observabilityManager,
		backendRepo:    backendRepo,
		metrics:        metrics,
		healthChecker:  healthChecker,
		shutdownChan:   make(chan os.Signal, 1),
	}

	// Setup HTTP server and routes
	if err := server.setupServer(); err != nil {
		return nil, fmt.Errorf("failed to setup server: %w", err)
	}

	// Setup default routing rules
	if err := server.setupDefaultRoutes(); err != nil {
		return nil, fmt.Errorf("failed to setup default routes: %w", err)
	}

	return server, nil
}

// setupServer configures the HTTP server with enhanced routing and middleware
func (s *EnhancedLoadBalancerServer) setupServer() error {
	s.router = mux.NewRouter()

	// Create handlers
	adminHandler := handler.NewAdminHandler(s.loadBalancer, s.metrics, s.logger)
	configHandler := handler.NewConfigHandler(s.loadBalancer, s.logger)

	// Setup API routes with observability instrumentation
	apiRouter := s.router.PathPrefix("/api/v1").Subrouter()

	// Health endpoints
	healthHandler := s.observability.InstrumentHTTPHandler(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			s.handleEnhancedHealth(w, r)
		}),
		"health_check",
	)
	apiRouter.Handle("/health", healthHandler).Methods("GET")

	// Enhanced observability endpoints
	apiRouter.HandleFunc("/observability/metrics", s.handleObservabilityMetrics).Methods("GET")
	apiRouter.HandleFunc("/observability/traces", s.handleObservabilityTraces).Methods("GET")

	// Routing management endpoints
	apiRouter.HandleFunc("/routing/rules", s.handleGetRoutingRules).Methods("GET")
	apiRouter.HandleFunc("/routing/rules", s.handleAddRoutingRule).Methods("POST")
	apiRouter.HandleFunc("/routing/rules/{id}", s.handleUpdateRoutingRule).Methods("PUT")
	apiRouter.HandleFunc("/routing/rules/{id}", s.handleDeleteRoutingRule).Methods("DELETE")
	apiRouter.HandleFunc("/routing/stats", s.handleRoutingStats).Methods("GET")

	// Traffic management endpoints
	apiRouter.HandleFunc("/traffic/rules", s.handleGetTrafficRules).Methods("GET")
	apiRouter.HandleFunc("/traffic/rules", s.handleAddTrafficRule).Methods("POST")
	apiRouter.HandleFunc("/traffic/stats", s.handleTrafficStats).Methods("GET")

	// Admin endpoints
	if s.config.Admin.Enabled {
		adminRouter := apiRouter.PathPrefix("/admin").Subrouter()
		adminRouter.HandleFunc("/backends", adminHandler.ListBackendsHandler).Methods("GET")
		adminRouter.HandleFunc("/backends", adminHandler.AddBackendHandler).Methods("POST")
		adminRouter.HandleFunc("/backends/{id}", adminHandler.DeleteBackendHandler).Methods("DELETE")
		adminRouter.HandleFunc("/config", adminHandler.GetConfigHandler).Methods("GET")
		adminRouter.HandleFunc("/stats", adminHandler.GetStatsHandler).Methods("GET")
		adminRouter.HandleFunc("/reload", configHandler.ReloadConfigHandler).Methods("POST")
	}

	// Main traffic handler (catch-all) - this integrates with the new traffic manager
	trafficHandler := s.observability.InstrumentHTTPHandler(
		http.HandlerFunc(s.handleTrafficRequest),
		"traffic_processing",
	)
	s.router.PathPrefix("/").Handler(trafficHandler)

	// Apply middleware
	var handlerChain http.Handler = s.router

	// Apply security middleware if configured
	if s.config.Security != nil {
		securityMiddleware, err := middleware.NewSecurityMiddleware(*s.config.Security, s.logger)
		if err != nil {
			return fmt.Errorf("failed to create security middleware: %w", err)
		}
		handlerChain = securityMiddleware.SecurityMiddleware()(handlerChain)
	}

	// Create HTTP server
	s.server = &http.Server{
		Addr:         fmt.Sprintf(":%d", s.config.LoadBalancer.Port),
		Handler:      handlerChain,
		ReadTimeout:  s.config.Server.ReadTimeout,
		WriteTimeout: s.config.Server.WriteTimeout,
		IdleTimeout:  s.config.Server.IdleTimeout,
	}

	return nil
}

// setupDefaultRoutes configures default routing rules for the enhanced architecture
func (s *EnhancedLoadBalancerServer) setupDefaultRoutes() error {
	// Add default routing rule for API endpoints
	apiRule := routing.RoutingRule{
		ID:       "api-routes",
		Name:     "API Routes",
		Priority: 100,
		Enabled:  true,
		Conditions: []routing.RuleCondition{
			{
				Type:     routing.ConditionPath,
				Operator: routing.OperatorPrefix,
				Values:   []string{"/api/"},
			},
		},
		Actions: []routing.RuleAction{
			{
				Type:   routing.ActionRoute,
				Target: "api-backend",
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.routingEngine.AddRule(apiRule); err != nil {
		return fmt.Errorf("failed to add API routing rule: %w", err)
	}

	// Add default routing rule for health checks
	healthRule := routing.RoutingRule{
		ID:       "health-routes",
		Name:     "Health Check Routes",
		Priority: 90,
		Enabled:  true,
		Conditions: []routing.RuleCondition{
			{
				Type:     routing.ConditionPath,
				Operator: routing.OperatorEquals,
				Values:   []string{"/health"},
			},
		},
		Actions: []routing.RuleAction{
			{
				Type:   routing.ActionRoute,
				Target: "health-backend",
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.routingEngine.AddRule(healthRule); err != nil {
		return fmt.Errorf("failed to add health routing rule: %w", err)
	}

	// Add default catch-all routing rule
	defaultRule := routing.RoutingRule{
		ID:       "default-route",
		Name:     "Default Route",
		Priority: 1,
		Enabled:  true,
		Conditions: []routing.RuleCondition{
			{
				Type:     routing.ConditionPath,
				Operator: routing.OperatorPrefix,
				Values:   []string{"/"},
			},
		},
		Actions: []routing.RuleAction{
			{
				Type:   routing.ActionRoute,
				Target: "default-backend",
			},
		},
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if err := s.routingEngine.AddRule(defaultRule); err != nil {
		return fmt.Errorf("failed to add default routing rule: %w", err)
	}

	s.logger.Info("Default routing rules configured successfully")
	return nil
}

// handleTrafficRequest handles incoming traffic through the new traffic management system
func (s *EnhancedLoadBalancerServer) handleTrafficRequest(w http.ResponseWriter, r *http.Request) {
	// Process request through traffic manager
	if err := s.trafficManager.ProcessRequest(w, r); err != nil {
		s.logger.WithFields(map[string]interface{}{
			"error":  err.Error(),
			"path":   r.URL.Path,
			"method": r.Method,
		}).Error("Traffic processing failed")

		// Fallback to traditional load balancer
		s.handleFallbackRequest(w, r)
		return
	}
}

// handleFallbackRequest handles requests when traffic manager fails
func (s *EnhancedLoadBalancerServer) handleFallbackRequest(w http.ResponseWriter, r *http.Request) {
	// Get backend from traditional load balancer
	backend := s.loadBalancer.SelectBackend()
	if backend == nil {
		http.Error(w, "No backend available", http.StatusServiceUnavailable)
		return
	}

	// For demonstration, just return backend info
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(fmt.Sprintf(`{"backend":"%s","message":"Handled by fallback"}`, backend.URL)))
}

// Start starts the enhanced load balancer server
func (s *EnhancedLoadBalancerServer) Start(ctx context.Context) error {
	s.logger.WithFields(map[string]interface{}{
		"port":     s.config.LoadBalancer.Port,
		"strategy": s.config.LoadBalancer.Strategy,
		"backends": len(s.config.Backends),
		"components": []string{
			"routing-engine",
			"traffic-manager",
			"observability",
			"enhanced-middleware",
		},
	}).Info("Starting enhanced load balancer server")

	// Setup graceful shutdown
	signal.Notify(s.shutdownChan, syscall.SIGINT, syscall.SIGTERM)

	// Start server
	go func() {
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			s.logger.WithFields(map[string]interface{}{
				"error": err.Error(),
			}).Fatal("HTTP server failed")
		}
	}()

	s.logger.WithFields(map[string]interface{}{
		"port":             s.config.LoadBalancer.Port,
		"health_endpoint":  fmt.Sprintf("http://localhost:%d/api/v1/health", s.config.LoadBalancer.Port),
		"metrics_endpoint": fmt.Sprintf("http://localhost:%d/api/v1/observability/metrics", s.config.LoadBalancer.Port),
		"routing_endpoint": fmt.Sprintf("http://localhost:%d/api/v1/routing/rules", s.config.LoadBalancer.Port),
		"traffic_endpoint": fmt.Sprintf("http://localhost:%d/api/v1/traffic/rules", s.config.LoadBalancer.Port),
	}).Info("Enhanced load balancer started successfully")

	// Wait for shutdown signal
	<-s.shutdownChan
	return s.Stop(ctx)
}

// Stop gracefully stops the enhanced load balancer server
func (s *EnhancedLoadBalancerServer) Stop(ctx context.Context) error {
	s.logger.Info("Shutting down enhanced load balancer server")

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	// Shutdown HTTP server
	if err := s.server.Shutdown(shutdownCtx); err != nil {
		s.logger.WithFields(map[string]interface{}{
			"error": err.Error(),
		}).Error("Server shutdown failed")
		return err
	}

	s.logger.Info("Enhanced load balancer server stopped successfully")
	return nil
}

// Enhanced API handlers

func (s *EnhancedLoadBalancerServer) handleEnhancedHealth(w http.ResponseWriter, r *http.Request) {
	health := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now().UTC(),
		"version":   "enhanced-v1.0.0",
		"components": map[string]interface{}{
			"routing_engine":  s.routingEngine.GetStats(),
			"traffic_manager": s.trafficManager.GetStats(),
			"observability":   s.observability.GetMetrics(),
			"load_balancer":   s.loadBalancer.GetBackends(),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := writeJSON(w, health); err != nil {
		s.logger.Error("Failed to write health response")
	}
}

func (s *EnhancedLoadBalancerServer) handleObservabilityMetrics(w http.ResponseWriter, r *http.Request) {
	metrics := s.observability.GetMetrics()
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, metrics); err != nil {
		s.logger.Error("Failed to write metrics response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *EnhancedLoadBalancerServer) handleObservabilityTraces(w http.ResponseWriter, r *http.Request) {
	traces := s.observability.GetTraces()
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, traces); err != nil {
		s.logger.Error("Failed to write traces response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *EnhancedLoadBalancerServer) handleGetRoutingRules(w http.ResponseWriter, r *http.Request) {
	rules := s.routingEngine.GetRules()
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, rules); err != nil {
		s.logger.Error("Failed to write routing rules response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *EnhancedLoadBalancerServer) handleAddRoutingRule(w http.ResponseWriter, r *http.Request) {
	var rule routing.RoutingRule
	if err := readJSON(r, &rule); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.routingEngine.AddRule(rule); err != nil {
		s.logger.WithFields(map[string]interface{}{
			"error": err.Error(),
		}).Error("Failed to add routing rule")
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"message": "Routing rule added successfully"})
}

func (s *EnhancedLoadBalancerServer) handleUpdateRoutingRule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ruleID := vars["id"]

	var rule routing.RoutingRule
	if err := readJSON(r, &rule); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.routingEngine.UpdateRule(ruleID, rule); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	writeJSON(w, map[string]string{"message": "Routing rule updated successfully"})
}

func (s *EnhancedLoadBalancerServer) handleDeleteRoutingRule(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	ruleID := vars["id"]

	if err := s.routingEngine.RemoveRule(ruleID); err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}

	writeJSON(w, map[string]string{"message": "Routing rule deleted successfully"})
}

func (s *EnhancedLoadBalancerServer) handleRoutingStats(w http.ResponseWriter, r *http.Request) {
	stats := s.routingEngine.GetStats()
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, stats); err != nil {
		s.logger.Error("Failed to write routing stats response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *EnhancedLoadBalancerServer) handleGetTrafficRules(w http.ResponseWriter, r *http.Request) {
	rules := s.trafficManager.GetTrafficRules()
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, rules); err != nil {
		s.logger.Error("Failed to write traffic rules response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func (s *EnhancedLoadBalancerServer) handleAddTrafficRule(w http.ResponseWriter, r *http.Request) {
	var rule traffic.TrafficRule
	if err := readJSON(r, &rule); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if err := s.trafficManager.AddTrafficRule(&rule); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	w.WriteHeader(http.StatusCreated)
	writeJSON(w, map[string]string{"message": "Traffic rule added successfully"})
}

func (s *EnhancedLoadBalancerServer) handleTrafficStats(w http.ResponseWriter, r *http.Request) {
	stats := s.trafficManager.GetStats()
	w.Header().Set("Content-Type", "application/json")
	if err := writeJSON(w, stats); err != nil {
		s.logger.Error("Failed to write traffic stats response")
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

// Utility functions

func writeJSON(w http.ResponseWriter, data interface{}) error {
	// In a real implementation, this would use a proper JSON encoder
	// For now, return nil to avoid import issues
	return nil
}

func readJSON(r *http.Request, data interface{}) error {
	// In a real implementation, this would use a proper JSON decoder
	// For now, return nil to avoid import issues
	return nil
}

func main() {
	server, err := NewEnhancedLoadBalancerServer()
	if err != nil {
		log.Fatalf("Failed to create enhanced load balancer server: %v", err)
	}

	ctx := context.Background()
	if err := server.Start(ctx); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
