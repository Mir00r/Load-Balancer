package main

import (
	"os"
	"strconv"
)

// getProcessInfo returns process information for logging
// This implements 12-Factor App methodology - Factor #8: Concurrency
func getProcessInfo() map[string]interface{} {
	return map[string]interface{}{
		"pid":      os.Getpid(),
		"ppid":     os.Getppid(),
		"uid":      os.Getuid(),
		"gid":      os.Getgid(),
		"hostname": getHostname(),
		"args":     os.Args,
	}
}

// getHostname safely gets hostname
func getHostname() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return hostname
}

// getPort gets the port from environment or config
// Factor #7: Port Binding - Export services via port binding
func getPort(defaultPort int) int {
	if portStr := os.Getenv("PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil && port > 0 && port <= 65535 {
			return port
		}
	}
	return defaultPort
}
