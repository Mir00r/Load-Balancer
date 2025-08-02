#!/bin/bash

# Stop all backend servers

echo "Stopping backend servers..."

pkill -f "examples/backend/main.go"

echo "All backend servers stopped!"
