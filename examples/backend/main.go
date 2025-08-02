package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"
)

func main() {
	port := 8081
	if len(os.Args) > 1 {
		if p, err := strconv.Atoi(os.Args[1]); err == nil {
			port = p
		}
	}

	id := fmt.Sprintf("backend-%d", port)

	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().UTC(),
			"backend":   id,
			"port":      port,
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	})

	// Default handler
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"message":     "Hello from backend server!",
			"backend":     id,
			"port":        port,
			"path":        r.URL.Path,
			"method":      r.Method,
			"timestamp":   time.Now().UTC(),
			"headers":     r.Header,
			"remote_addr": r.RemoteAddr,
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Backend-ID", id)
		json.NewEncoder(w).Encode(response)
	})

	// API endpoint
	mux.HandleFunc("/api/data", func(w http.ResponseWriter, r *http.Request) {
		response := map[string]interface{}{
			"data": map[string]interface{}{
				"items": []string{"item1", "item2", "item3"},
				"count": 3,
			},
			"backend":   id,
			"timestamp": time.Now().UTC(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Backend-ID", id)
		json.NewEncoder(w).Encode(response)
	})

	// Slow endpoint for testing
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		delay := 2 * time.Second
		if d := r.URL.Query().Get("delay"); d != "" {
			if parsed, err := time.ParseDuration(d); err == nil {
				delay = parsed
			}
		}

		time.Sleep(delay)

		response := map[string]interface{}{
			"message": "Slow response completed",
			"backend": id,
			"delay":   delay.String(),
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Backend-ID", id)
		json.NewEncoder(w).Encode(response)
	})

	// Error endpoint for testing
	mux.HandleFunc("/error", func(w http.ResponseWriter, r *http.Request) {
		statusCode := http.StatusInternalServerError
		if code := r.URL.Query().Get("code"); code != "" {
			if parsed, err := strconv.Atoi(code); err == nil {
				statusCode = parsed
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Backend-ID", id)
		w.WriteHeader(statusCode)

		response := map[string]interface{}{
			"error":   "Simulated error response",
			"backend": id,
			"code":    statusCode,
		}
		json.NewEncoder(w).Encode(response)
	})

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	log.Printf("Starting backend server %s on port %d", id, port)
	log.Fatal(server.ListenAndServe())
}
