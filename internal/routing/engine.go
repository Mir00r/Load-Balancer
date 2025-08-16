// Package routing provides a central routing engine inspired by Traefik's architecture.
// This package implements intelligent request routing based on configurable rules,
// supporting path-based, host-based, header-based, and custom routing logic.
package routing

import (
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mir00r/load-balancer/pkg/logger"
)

// ConditionType defines the type of routing condition
type ConditionType string

const (
	ConditionHost      ConditionType = "host"
	ConditionPath      ConditionType = "path"
	ConditionMethod    ConditionType = "method"
	ConditionHeader    ConditionType = "header"
	ConditionQuery     ConditionType = "query"
	ConditionScheme    ConditionType = "scheme"
	ConditionRemoteIP  ConditionType = "remote_ip"
	ConditionUserAgent ConditionType = "user_agent"
)

// OperatorType defines the matching operator for conditions
type OperatorType string

const (
	OperatorEquals      OperatorType = "equals"
	OperatorContains    OperatorType = "contains"
	OperatorRegex       OperatorType = "regex"
	OperatorPrefix      OperatorType = "prefix"
	OperatorSuffix      OperatorType = "suffix"
	OperatorIn          OperatorType = "in"
	OperatorNotEquals   OperatorType = "not_equals"
	OperatorNotContains OperatorType = "not_contains"
)

// ActionType defines the type of action to take when a rule matches
type ActionType string

const (
	ActionRoute        ActionType = "route"
	ActionRedirect     ActionType = "redirect"
	ActionRewrite      ActionType = "rewrite"
	ActionBlock        ActionType = "block"
	ActionRateLimit    ActionType = "rate_limit"
	ActionAddHeader    ActionType = "add_header"
	ActionRemoveHeader ActionType = "remove_header"
)

// RuleCondition represents a single condition in a routing rule
type RuleCondition struct {
	Type     ConditionType `json:"type" yaml:"type"`
	Operator OperatorType  `json:"operator" yaml:"operator"`
	Key      string        `json:"key,omitempty" yaml:"key,omitempty"` // For headers, query params
	Values   []string      `json:"values" yaml:"values"`
	Negate   bool          `json:"negate,omitempty" yaml:"negate,omitempty"`

	// Compiled regex for performance
	compiledRegex *regexp.Regexp `json:"-" yaml:"-"`
}

// RuleAction represents an action to take when a rule matches
type RuleAction struct {
	Type   ActionType        `json:"type" yaml:"type"`
	Target string            `json:"target,omitempty" yaml:"target,omitempty"` // Backend, URL, etc.
	Params map[string]string `json:"params,omitempty" yaml:"params,omitempty"` // Additional parameters
}

// RoutingRule represents a complete routing rule with conditions and actions
type RoutingRule struct {
	ID          string          `json:"id" yaml:"id"`
	Name        string          `json:"name" yaml:"name"`
	Description string          `json:"description,omitempty" yaml:"description,omitempty"`
	Priority    int             `json:"priority" yaml:"priority"`
	Enabled     bool            `json:"enabled" yaml:"enabled"`
	Conditions  []RuleCondition `json:"conditions" yaml:"conditions"`
	Actions     []RuleAction    `json:"actions" yaml:"actions"`
	Middlewares []string        `json:"middlewares,omitempty" yaml:"middlewares,omitempty"`

	// Metadata
	CreatedAt time.Time         `json:"created_at" yaml:"created_at"`
	UpdatedAt time.Time         `json:"updated_at" yaml:"updated_at"`
	Tags      map[string]string `json:"tags,omitempty" yaml:"tags,omitempty"`

	// Statistics
	MatchCount  int64     `json:"match_count" yaml:"match_count"`
	LastMatched time.Time `json:"last_matched,omitempty" yaml:"last_matched,omitempty"`
}

// RoutingResult represents the result of routing decision
type RoutingResult struct {
	MatchedRule *RoutingRule  `json:"matched_rule,omitempty"`
	Actions     []RuleAction  `json:"actions"`
	Backend     string        `json:"backend,omitempty"`
	Middlewares []string      `json:"middlewares,omitempty"`
	Headers     http.Header   `json:"headers,omitempty"`
	StatusCode  int           `json:"status_code,omitempty"`
	Message     string        `json:"message,omitempty"`
	ProcessTime time.Duration `json:"process_time"`
}

// RoutingStats provides statistics for the routing engine
type RoutingStats struct {
	TotalRequests     int64 `json:"total_requests"`
	MatchedRequests   int64 `json:"matched_requests"`
	UnmatchedRequests int64 `json:"unmatched_requests"`
	ErrorRequests     int64 `json:"error_requests"`

	// Performance metrics
	AverageProcessTime time.Duration `json:"average_process_time"`
	MaxProcessTime     time.Duration `json:"max_process_time"`
	MinProcessTime     time.Duration `json:"min_process_time"`

	// Rule statistics
	TotalRules    int `json:"total_rules"`
	EnabledRules  int `json:"enabled_rules"`
	DisabledRules int `json:"disabled_rules"`
}

// RouteEngine is the central routing engine that processes requests and applies routing rules
type RouteEngine struct {
	rules      []RoutingRule
	rulesMutex sync.RWMutex

	stats      RoutingStats
	statsMutex sync.RWMutex

	logger *logger.Logger

	// Performance optimization
	hostRules   map[string][]int // Pre-indexed rules by host
	pathRules   map[string][]int // Pre-indexed rules by path prefix
	methodRules map[string][]int // Pre-indexed rules by HTTP method
}

// NewRouteEngine creates a new routing engine with the provided configuration
func NewRouteEngine(logger *logger.Logger) *RouteEngine {
	return &RouteEngine{
		rules:       make([]RoutingRule, 0),
		logger:      logger,
		hostRules:   make(map[string][]int),
		pathRules:   make(map[string][]int),
		methodRules: make(map[string][]int),
		stats: RoutingStats{
			MinProcessTime: time.Hour, // Initialize with large value
		},
	}
}

// AddRule adds a new routing rule to the engine
func (re *RouteEngine) AddRule(rule RoutingRule) error {
	re.rulesMutex.Lock()
	defer re.rulesMutex.Unlock()

	// Validate rule
	if err := re.validateRule(rule); err != nil {
		return fmt.Errorf("invalid routing rule: %w", err)
	}

	// Compile regex patterns
	if err := re.compileRuleRegex(&rule); err != nil {
		return fmt.Errorf("failed to compile regex in rule: %w", err)
	}

	// Set metadata
	rule.CreatedAt = time.Now()
	rule.UpdatedAt = time.Now()

	// Add rule
	re.rules = append(re.rules, rule)

	// Sort rules by priority (higher priority first)
	sort.Slice(re.rules, func(i, j int) bool {
		return re.rules[i].Priority > re.rules[j].Priority
	})

	// Update indexes
	re.updateIndexes()

	// Update statistics
	re.updateRuleStats()

	re.logger.WithFields(map[string]interface{}{
		"rule_id":   rule.ID,
		"rule_name": rule.Name,
		"priority":  rule.Priority,
	}).Info("Routing rule added")

	return nil
}

// RemoveRule removes a routing rule by ID
func (re *RouteEngine) RemoveRule(ruleID string) error {
	re.rulesMutex.Lock()
	defer re.rulesMutex.Unlock()

	for i, rule := range re.rules {
		if rule.ID == ruleID {
			// Remove rule
			re.rules = append(re.rules[:i], re.rules[i+1:]...)

			// Update indexes
			re.updateIndexes()

			// Update statistics
			re.updateRuleStats()

			re.logger.WithFields(map[string]interface{}{
				"rule_id":   rule.ID,
				"rule_name": rule.Name,
			}).Info("Routing rule removed")

			return nil
		}
	}

	return fmt.Errorf("routing rule with ID %s not found", ruleID)
}

// UpdateRule updates an existing routing rule
func (re *RouteEngine) UpdateRule(ruleID string, updatedRule RoutingRule) error {
	re.rulesMutex.Lock()
	defer re.rulesMutex.Unlock()

	for i, rule := range re.rules {
		if rule.ID == ruleID {
			// Validate updated rule
			if err := re.validateRule(updatedRule); err != nil {
				return fmt.Errorf("invalid updated routing rule: %w", err)
			}

			// Compile regex patterns
			if err := re.compileRuleRegex(&updatedRule); err != nil {
				return fmt.Errorf("failed to compile regex in updated rule: %w", err)
			}

			// Preserve metadata
			updatedRule.ID = rule.ID
			updatedRule.CreatedAt = rule.CreatedAt
			updatedRule.UpdatedAt = time.Now()
			updatedRule.MatchCount = rule.MatchCount
			updatedRule.LastMatched = rule.LastMatched

			// Update rule
			re.rules[i] = updatedRule

			// Re-sort rules by priority
			sort.Slice(re.rules, func(i, j int) bool {
				return re.rules[i].Priority > re.rules[j].Priority
			})

			// Update indexes
			re.updateIndexes()

			re.logger.WithFields(map[string]interface{}{
				"rule_id":   updatedRule.ID,
				"rule_name": updatedRule.Name,
			}).Info("Routing rule updated")

			return nil
		}
	}

	return fmt.Errorf("routing rule with ID %s not found", ruleID)
}

// RouteRequest processes an incoming request and returns routing decision
func (re *RouteEngine) RouteRequest(r *http.Request) *RoutingResult {
	startTime := time.Now()

	// Update request statistics
	re.updateRequestStats(true)
	defer func() {
		processTime := time.Since(startTime)
		re.updateProcessTime(processTime)
	}()

	re.rulesMutex.RLock()
	defer re.rulesMutex.RUnlock()

	// Try to find matching rule
	for i := range re.rules {
		rule := &re.rules[i]

		if !rule.Enabled {
			continue
		}

		if re.matchRule(rule, r) {
			// Update rule statistics
			re.updateRuleMatchStats(rule)

			// Create routing result
			result := &RoutingResult{
				MatchedRule: rule,
				Actions:     rule.Actions,
				Middlewares: rule.Middlewares,
				ProcessTime: time.Since(startTime),
			}

			// Process actions
			re.processActions(result, r)

			re.updateRequestStats(false)

			re.logger.WithFields(map[string]interface{}{
				"rule_id":      rule.ID,
				"rule_name":    rule.Name,
				"request_path": r.URL.Path,
				"request_host": r.Host,
				"process_time": result.ProcessTime,
			}).Debug("Request matched routing rule")

			return result
		}
	}

	// No rule matched
	re.statsMutex.Lock()
	re.stats.UnmatchedRequests++
	re.statsMutex.Unlock()

	return &RoutingResult{
		Actions:     []RuleAction{{Type: ActionRoute, Target: "default"}},
		ProcessTime: time.Since(startTime),
		Message:     "No routing rule matched",
	}
}

// GetRules returns all routing rules
func (re *RouteEngine) GetRules() []RoutingRule {
	re.rulesMutex.RLock()
	defer re.rulesMutex.RUnlock()

	// Return a copy to prevent modification
	rules := make([]RoutingRule, len(re.rules))
	copy(rules, re.rules)
	return rules
}

// GetStats returns routing engine statistics
func (re *RouteEngine) GetStats() RoutingStats {
	re.statsMutex.RLock()
	defer re.statsMutex.RUnlock()

	return re.stats
}

// validateRule validates a routing rule
func (re *RouteEngine) validateRule(rule RoutingRule) error {
	if rule.ID == "" {
		return fmt.Errorf("rule ID is required")
	}

	if rule.Name == "" {
		return fmt.Errorf("rule name is required")
	}

	if len(rule.Conditions) == 0 {
		return fmt.Errorf("rule must have at least one condition")
	}

	if len(rule.Actions) == 0 {
		return fmt.Errorf("rule must have at least one action")
	}

	// Validate conditions
	for _, condition := range rule.Conditions {
		if err := re.validateCondition(condition); err != nil {
			return fmt.Errorf("invalid condition: %w", err)
		}
	}

	// Validate actions
	for _, action := range rule.Actions {
		if err := re.validateAction(action); err != nil {
			return fmt.Errorf("invalid action: %w", err)
		}
	}

	return nil
}

// validateCondition validates a single rule condition
func (re *RouteEngine) validateCondition(condition RuleCondition) error {
	// Validate condition type
	validTypes := []ConditionType{
		ConditionHost, ConditionPath, ConditionMethod, ConditionHeader,
		ConditionQuery, ConditionScheme, ConditionRemoteIP, ConditionUserAgent,
	}

	typeValid := false
	for _, validType := range validTypes {
		if condition.Type == validType {
			typeValid = true
			break
		}
	}
	if !typeValid {
		return fmt.Errorf("invalid condition type: %s", condition.Type)
	}

	// Validate operator
	validOperators := []OperatorType{
		OperatorEquals, OperatorContains, OperatorRegex, OperatorPrefix,
		OperatorSuffix, OperatorIn, OperatorNotEquals, OperatorNotContains,
	}

	operatorValid := false
	for _, validOperator := range validOperators {
		if condition.Operator == validOperator {
			operatorValid = true
			break
		}
	}
	if !operatorValid {
		return fmt.Errorf("invalid operator: %s", condition.Operator)
	}

	// Validate values
	if len(condition.Values) == 0 {
		return fmt.Errorf("condition must have at least one value")
	}

	// Validate regex patterns
	if condition.Operator == OperatorRegex {
		for _, value := range condition.Values {
			if _, err := regexp.Compile(value); err != nil {
				return fmt.Errorf("invalid regex pattern '%s': %w", value, err)
			}
		}
	}

	return nil
}

// validateAction validates a single rule action
func (re *RouteEngine) validateAction(action RuleAction) error {
	// Validate action type
	validTypes := []ActionType{
		ActionRoute, ActionRedirect, ActionRewrite, ActionBlock,
		ActionRateLimit, ActionAddHeader, ActionRemoveHeader,
	}

	typeValid := false
	for _, validType := range validTypes {
		if action.Type == validType {
			typeValid = true
			break
		}
	}
	if !typeValid {
		return fmt.Errorf("invalid action type: %s", action.Type)
	}

	// Validate required fields based on action type
	switch action.Type {
	case ActionRoute:
		if action.Target == "" {
			return fmt.Errorf("route action requires target backend")
		}
	case ActionRedirect:
		if action.Target == "" {
			return fmt.Errorf("redirect action requires target URL")
		}
	case ActionAddHeader, ActionRemoveHeader:
		if action.Params == nil || action.Params["name"] == "" {
			return fmt.Errorf("header action requires header name in params")
		}
	}

	return nil
}

// compileRuleRegex compiles regex patterns in rule conditions for performance
func (re *RouteEngine) compileRuleRegex(rule *RoutingRule) error {
	for i := range rule.Conditions {
		condition := &rule.Conditions[i]
		if condition.Operator == OperatorRegex {
			for _, value := range condition.Values {
				compiled, err := regexp.Compile(value)
				if err != nil {
					return fmt.Errorf("failed to compile regex '%s': %w", value, err)
				}
				condition.compiledRegex = compiled
				break // Use first value for compiled regex
			}
		}
	}
	return nil
}

// updateIndexes updates performance indexes for faster rule matching
func (re *RouteEngine) updateIndexes() {
	// Clear existing indexes
	re.hostRules = make(map[string][]int)
	re.pathRules = make(map[string][]int)
	re.methodRules = make(map[string][]int)

	// Build new indexes
	for i, rule := range re.rules {
		if !rule.Enabled {
			continue
		}

		for _, condition := range rule.Conditions {
			switch condition.Type {
			case ConditionHost:
				for _, value := range condition.Values {
					re.hostRules[value] = append(re.hostRules[value], i)
				}
			case ConditionPath:
				if condition.Operator == OperatorPrefix {
					for _, value := range condition.Values {
						re.pathRules[value] = append(re.pathRules[value], i)
					}
				}
			case ConditionMethod:
				for _, value := range condition.Values {
					re.methodRules[value] = append(re.methodRules[value], i)
				}
			}
		}
	}
}

// matchRule checks if a request matches a routing rule
func (re *RouteEngine) matchRule(rule *RoutingRule, r *http.Request) bool {
	// All conditions must match (AND logic)
	for _, condition := range rule.Conditions {
		if !re.matchCondition(condition, r) {
			return false
		}
	}
	return true
}

// matchCondition checks if a request matches a single condition
func (re *RouteEngine) matchCondition(condition RuleCondition, r *http.Request) bool {
	var requestValue string

	// Extract request value based on condition type
	switch condition.Type {
	case ConditionHost:
		requestValue = r.Host
	case ConditionPath:
		requestValue = r.URL.Path
	case ConditionMethod:
		requestValue = r.Method
	case ConditionScheme:
		requestValue = r.URL.Scheme
		if requestValue == "" {
			if r.TLS != nil {
				requestValue = "https"
			} else {
				requestValue = "http"
			}
		}
	case ConditionHeader:
		requestValue = r.Header.Get(condition.Key)
	case ConditionQuery:
		requestValue = r.URL.Query().Get(condition.Key)
	case ConditionRemoteIP:
		requestValue = r.RemoteAddr
	case ConditionUserAgent:
		requestValue = r.Header.Get("User-Agent")
	default:
		return false
	}

	// Check if value matches condition
	matched := re.matchValue(condition, requestValue)

	// Apply negation if specified
	if condition.Negate {
		matched = !matched
	}

	return matched
}

// matchValue checks if a value matches condition criteria
func (re *RouteEngine) matchValue(condition RuleCondition, value string) bool {
	for _, conditionValue := range condition.Values {
		var matched bool

		switch condition.Operator {
		case OperatorEquals:
			matched = value == conditionValue
		case OperatorContains:
			matched = strings.Contains(value, conditionValue)
		case OperatorPrefix:
			matched = strings.HasPrefix(value, conditionValue)
		case OperatorSuffix:
			matched = strings.HasSuffix(value, conditionValue)
		case OperatorRegex:
			if condition.compiledRegex != nil {
				matched = condition.compiledRegex.MatchString(value)
			}
		case OperatorNotEquals:
			matched = value != conditionValue
		case OperatorNotContains:
			matched = !strings.Contains(value, conditionValue)
		case OperatorIn:
			matched = strings.Contains(conditionValue, value)
		}

		if matched {
			return true
		}
	}
	return false
}

// processActions processes rule actions and updates the routing result
func (re *RouteEngine) processActions(result *RoutingResult, r *http.Request) {
	for _, action := range result.Actions {
		switch action.Type {
		case ActionRoute:
			result.Backend = action.Target
		case ActionRedirect:
			result.StatusCode = 302
			if code, exists := action.Params["code"]; exists {
				if code == "301" {
					result.StatusCode = 301
				} else if code == "303" {
					result.StatusCode = 303
				}
			}
			if result.Headers == nil {
				result.Headers = make(http.Header)
			}
			result.Headers.Set("Location", action.Target)
		case ActionBlock:
			result.StatusCode = 403
			result.Message = "Request blocked by routing rule"
		case ActionAddHeader:
			if result.Headers == nil {
				result.Headers = make(http.Header)
			}
			headerName := action.Params["name"]
			headerValue := action.Params["value"]
			result.Headers.Set(headerName, headerValue)
		}
	}
}

// updateRequestStats updates request-related statistics
func (re *RouteEngine) updateRequestStats(increment bool) {
	re.statsMutex.Lock()
	defer re.statsMutex.Unlock()

	if increment {
		re.stats.TotalRequests++
	} else {
		re.stats.MatchedRequests++
	}
}

// updateProcessTime updates processing time statistics
func (re *RouteEngine) updateProcessTime(processTime time.Duration) {
	re.statsMutex.Lock()
	defer re.statsMutex.Unlock()

	if processTime > re.stats.MaxProcessTime {
		re.stats.MaxProcessTime = processTime
	}

	if processTime < re.stats.MinProcessTime {
		re.stats.MinProcessTime = processTime
	}

	// Calculate moving average (simplified)
	if re.stats.TotalRequests > 0 {
		re.stats.AverageProcessTime = time.Duration(
			(int64(re.stats.AverageProcessTime)*int64(re.stats.TotalRequests-1) + int64(processTime)) / int64(re.stats.TotalRequests),
		)
	} else {
		re.stats.AverageProcessTime = processTime
	}
}

// updateRuleMatchStats updates statistics when a rule matches
func (re *RouteEngine) updateRuleMatchStats(rule *RoutingRule) {
	rule.MatchCount++
	rule.LastMatched = time.Now()
}

// updateRuleStats updates rule count statistics
func (re *RouteEngine) updateRuleStats() {
	re.statsMutex.Lock()
	defer re.statsMutex.Unlock()

	re.stats.TotalRules = len(re.rules)
	re.stats.EnabledRules = 0
	re.stats.DisabledRules = 0

	for _, rule := range re.rules {
		if rule.Enabled {
			re.stats.EnabledRules++
		} else {
			re.stats.DisabledRules++
		}
	}
}
