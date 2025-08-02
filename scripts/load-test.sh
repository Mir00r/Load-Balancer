#!/bin/bash

# Simple load test script

LOAD_BALANCER_URL=${1:-"http://localhost:8080"}
CONCURRENT_USERS=${2:-10}
TOTAL_REQUESTS=${3:-100}

echo "Running load test against $LOAD_BALANCER_URL"
echo "Concurrent users: $CONCURRENT_USERS"
echo "Total requests: $TOTAL_REQUESTS"

# Function to make requests
make_requests() {
    local user_id=$1
    local requests_per_user=$((TOTAL_REQUESTS / CONCURRENT_USERS))
    
    for ((i=1; i<=requests_per_user; i++)); do
        response=$(curl -s -w "%{http_code}" -o /dev/null "$LOAD_BALANCER_URL/")
        echo "User $user_id - Request $i: HTTP $response"
        sleep 0.1
    done
}

# Start concurrent users
echo "Starting load test..."
start_time=$(date +%s)

for ((user=1; user<=CONCURRENT_USERS; user++)); do
    make_requests $user &
done

# Wait for all background jobs to complete
wait

end_time=$(date +%s)
duration=$((end_time - start_time))

echo "Load test completed in ${duration}s"

# Get some stats
echo ""
echo "Load balancer metrics:"
curl -s "$LOAD_BALANCER_URL/metrics" | jq '.' 2>/dev/null || curl -s "$LOAD_BALANCER_URL/metrics"

echo ""
echo "Load balancer health:"
curl -s "$LOAD_BALANCER_URL/health" | jq '.' 2>/dev/null || curl -s "$LOAD_BALANCER_URL/health"
