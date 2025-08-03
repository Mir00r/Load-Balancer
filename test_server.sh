#!/bin/bash

# Test script for Load Balancer
echo "🚀 Starting Load Balancer Server Test..."

# Start the server in background
./bin/load-balancer &
SERVER_PID=$!

# Wait for server to start
sleep 3

echo "📋 Testing server endpoints..."

# Test health endpoint
echo "1. Testing health endpoint..."
curl -s http://localhost:8080/health || echo "❌ Health endpoint failed"

echo -e "\n2. Testing enhanced health endpoint..."
curl -s http://localhost:8080/api/v1/health || echo "❌ Enhanced health endpoint failed"

echo -e "\n3. Testing Swagger documentation..."
curl -s -I http://localhost:8080/docs/ || echo "❌ Swagger docs failed"

echo -e "\n4. Testing admin API..."
curl -s http://localhost:8080/api/v1/admin/backends || echo "❌ Admin API failed"

echo -e "\n✅ Server test completed"

# Stop the server
kill $SERVER_PID 2>/dev/null
wait $SERVER_PID 2>/dev/null

echo "🛑 Server stopped"
