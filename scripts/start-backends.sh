#!/bin/bash

# Start multiple backend servers for testing

echo "Starting backend servers..."

# Start backend servers in background
go run examples/backend/main.go 8081 &
echo "Started backend-8081 (PID: $!)"

go run examples/backend/main.go 8082 &
echo "Started backend-8082 (PID: $!)"

go run examples/backend/main.go 8083 &
echo "Started backend-8083 (PID: $!)"

echo "All backend servers started!"
echo "Use 'pkill -f \"examples/backend/main.go\"' to stop all backends"
