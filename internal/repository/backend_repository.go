package repository

import (
	"fmt"
	"sync"

	"github.com/mir00r/load-balancer/internal/domain"
)

// InMemoryBackendRepository implements BackendRepository using in-memory storage
type InMemoryBackendRepository struct {
	mu       sync.RWMutex
	backends map[string]*domain.Backend
}

// NewInMemoryBackendRepository creates a new in-memory backend repository
func NewInMemoryBackendRepository() *InMemoryBackendRepository {
	return &InMemoryBackendRepository{
		backends: make(map[string]*domain.Backend),
	}
}

// GetAll returns all backends
func (r *InMemoryBackendRepository) GetAll() ([]*domain.Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	backends := make([]*domain.Backend, 0, len(r.backends))
	for _, backend := range r.backends {
		backends = append(backends, backend)
	}
	return backends, nil
}

// GetByID returns a backend by its ID
func (r *InMemoryBackendRepository) GetByID(id string) (*domain.Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	backend, exists := r.backends[id]
	if !exists {
		return nil, fmt.Errorf("backend with ID '%s' not found", id)
	}
	return backend, nil
}

// Save persists a backend
func (r *InMemoryBackendRepository) Save(backend *domain.Backend) error {
	if backend == nil {
		return fmt.Errorf("backend cannot be nil")
	}
	if backend.ID == "" {
		return fmt.Errorf("backend ID cannot be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.backends[backend.ID] = backend
	return nil
}

// Delete removes a backend
func (r *InMemoryBackendRepository) Delete(id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.backends[id]; !exists {
		return fmt.Errorf("backend with ID '%s' not found", id)
	}

	delete(r.backends, id)
	return nil
}

// GetHealthy returns only healthy backends
func (r *InMemoryBackendRepository) GetHealthy() ([]*domain.Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var healthyBackends []*domain.Backend
	for _, backend := range r.backends {
		if backend.IsHealthy() {
			healthyBackends = append(healthyBackends, backend)
		}
	}
	return healthyBackends, nil
}

// GetAvailable returns backends that can accept new connections
func (r *InMemoryBackendRepository) GetAvailable() ([]*domain.Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var availableBackends []*domain.Backend
	for _, backend := range r.backends {
		if backend.IsAvailable() {
			availableBackends = append(availableBackends, backend)
		}
	}
	return availableBackends, nil
}

// GetByStatus returns backends with the specified status
func (r *InMemoryBackendRepository) GetByStatus(status domain.BackendStatus) ([]*domain.Backend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var filteredBackends []*domain.Backend
	for _, backend := range r.backends {
		if backend.GetStatus() == status {
			filteredBackends = append(filteredBackends, backend)
		}
	}
	return filteredBackends, nil
}

// Count returns the total number of backends
func (r *InMemoryBackendRepository) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.backends)
}

// CountByStatus returns the number of backends with the specified status
func (r *InMemoryBackendRepository) CountByStatus(status domain.BackendStatus) int {
	r.mu.RLock()
	defer r.mu.RUnlock()

	count := 0
	for _, backend := range r.backends {
		if backend.GetStatus() == status {
			count++
		}
	}
	return count
}

// Exists checks if a backend with the given ID exists
func (r *InMemoryBackendRepository) Exists(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, exists := r.backends[id]
	return exists
}

// Clear removes all backends from the repository
func (r *InMemoryBackendRepository) Clear() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends = make(map[string]*domain.Backend)
	return nil
}

// SaveAll saves multiple backends in a single operation
func (r *InMemoryBackendRepository) SaveAll(backends []*domain.Backend) error {
	if backends == nil {
		return fmt.Errorf("backends slice cannot be nil")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Validate all backends first
	for i, backend := range backends {
		if backend == nil {
			return fmt.Errorf("backend at index %d cannot be nil", i)
		}
		if backend.ID == "" {
			return fmt.Errorf("backend at index %d has empty ID", i)
		}
	}

	// Save all backends
	for _, backend := range backends {
		r.backends[backend.ID] = backend
	}

	return nil
}

// GetStats returns repository statistics
func (r *InMemoryBackendRepository) GetStats() map[string]interface{} {
	r.mu.RLock()
	defer r.mu.RUnlock()

	stats := map[string]interface{}{
		"total_backends":       len(r.backends),
		"healthy_backends":     0,
		"unhealthy_backends":   0,
		"maintenance_backends": 0,
	}

	for _, backend := range r.backends {
		switch backend.GetStatus() {
		case domain.StatusHealthy:
			stats["healthy_backends"] = stats["healthy_backends"].(int) + 1
		case domain.StatusUnhealthy:
			stats["unhealthy_backends"] = stats["unhealthy_backends"].(int) + 1
		case domain.StatusMaintenance:
			stats["maintenance_backends"] = stats["maintenance_backends"].(int) + 1
		}
	}

	return stats
}
