# 🔄 Load Balancer Usage Guide

## Quick Start

### 1. Start the Load Balancer

```bash
# Option 1: Run directly
make run

# Option 2: Run in development mode
make run-dev

# Option 3: Build and run
make build
./build/load-balancer
```

### 2. Start Test Backend Servers

```bash
# Start all backend servers
make start-backends

# Or start manually
go run examples/backend/main.go 8081 &
go run examples/backend/main.go 8082 &
go run examples/backend/main.go 8083 &
```

### 3. Test the Load Balancer

```bash
# Send requests to the load balancer
curl http://localhost:8080/

# Check health status
curl http://localhost:8080/health

# View metrics
curl http://localhost:8080/metrics

# View backend status
curl http://localhost:8080/backends
```

## Configuration

### Load Balancing Strategies

1. **Round Robin** (default)
   ```yaml
   load_balancer:
     strategy: "round_robin"
   ```

2. **Weighted Round Robin**
   ```yaml
   load_balancer:
     strategy: "weighted_round_robin"
   backends:
     - id: "backend-1"
       weight: 1
     - id: "backend-2"
       weight: 2  # Gets 2x more traffic
   ```

3. **Least Connections**
   ```yaml
   load_balancer:
     strategy: "least_connections"
   ```

### Health Check Configuration

```yaml
load_balancer:
  health_check:
    enabled: true
    interval: 30s
    timeout: 5s
    healthy_threshold: 2
    unhealthy_threshold: 3
    path: "/health"
```

### Rate Limiting

```yaml
load_balancer:
  rate_limit:
    enabled: true
    requests_per_second: 100
    burst_size: 200
```

### Circuit Breaker

```yaml
load_balancer:
  circuit_breaker:
    enabled: true
    failure_threshold: 5
    recovery_timeout: 60s
    max_requests: 10
```

## API Endpoints

### Load Balancer Endpoints

- `GET /` - Proxies requests to backend servers
- `GET /health` - Load balancer health status
- `GET /metrics` - Performance metrics
- `GET /status` - General status information
- `GET /backends` - List all backends
- `POST /backends` - Add a new backend
- `DELETE /backends/{id}` - Remove a backend

### Backend Test Endpoints

- `GET /health` - Backend health check
- `GET /` - Basic response with backend info
- `GET /api/data` - Sample API endpoint
- `GET /slow?delay=2s` - Slow response for testing
- `GET /error?code=500` - Error response for testing

## Testing

### Load Testing

```bash
# Run basic load test
make load-test

# Custom load test
./scripts/load-test.sh http://localhost:8080 20 1000
```

### Unit Tests

```bash
# Run all tests
make test

# Run tests with verbose output
make test-verbose

# Run benchmarks
make bench
```

### Manual Testing

```bash
# Test round-robin distribution
for i in {1..6}; do
  curl -s http://localhost:8080/ | jq '.backend'
done

# Test health endpoint
curl -s http://localhost:8080/health | jq '.'

# Test metrics
curl -s http://localhost:8080/metrics | jq '.'

# Add a new backend
curl -X POST http://localhost:8080/backends \
  -H "Content-Type: application/json" \
  -d '{
    "id": "backend-4",
    "url": "http://localhost:8084",
    "weight": 1,
    "max_connections": 100
  }'

# Remove a backend
curl -X DELETE "http://localhost:8080/backends/backend-4"
```

## Docker Deployment

### Using Docker

```bash
# Build and run with Docker
make docker-build
make docker-run

# Or manually
docker build -t load-balancer .
docker run -p 8080:8080 load-balancer
```

### Using Docker Compose

```bash
# Start everything (load balancer + backends)
make docker-up

# Stop everything
make docker-down
```

## Monitoring and Observability

### Health Checks

The load balancer provides multiple health check endpoints:

```bash
# Overall health
curl http://localhost:8080/health

# Individual backend health
curl http://localhost:8080/backends
```

### Metrics

View detailed metrics:

```bash
curl http://localhost:8080/metrics | jq '.'
```

Metrics include:
- Total requests and errors
- Success rates per backend
- Response time distributions
- Active connections
- Circuit breaker status

### Logging

Structured JSON logging with configurable levels:

```yaml
logging:
  level: "info"    # trace, debug, info, warn, error, fatal, panic
  format: "json"   # json, text
  output: "stdout" # stdout, stderr, file
```

## Production Deployment

### Environment Variables

```bash
export CONFIG_FILE=/path/to/config.yaml
export LB_PORT=8080
export LB_STRATEGY=round_robin
export LB_LOG_LEVEL=info
```

### Systemd Service

Create `/etc/systemd/system/load-balancer.service`:

```ini
[Unit]
Description=Load Balancer
After=network.target

[Service]
Type=simple
User=loadbalancer
ExecStart=/usr/local/bin/load-balancer
Restart=always
RestartSec=5
Environment=CONFIG_FILE=/etc/load-balancer/config.yaml

[Install]
WantedBy=multi-user.target
```

### Nginx Frontend (Optional)

```nginx
upstream load_balancer {
    server 127.0.0.1:8080;
}

server {
    listen 80;
    server_name your-domain.com;
    
    location / {
        proxy_pass http://load_balancer;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }
}
```

## Troubleshooting

### Common Issues

1. **No healthy backends available**
   - Check backend server status
   - Verify health check configuration
   - Check network connectivity

2. **High error rates**
   - Review backend logs
   - Check circuit breaker status
   - Verify backend capacity

3. **Performance issues**
   - Monitor metrics endpoint
   - Check connection limits
   - Review rate limiting settings

### Debug Commands

```bash
# Check backend connectivity
curl http://localhost:8081/health

# View detailed logs
tail -f logs/load-balancer.log

# Check process status
ps aux | grep load-balancer

# Monitor real-time metrics
watch -n 1 'curl -s http://localhost:8080/metrics | jq ".total_requests"'
```

## Advanced Configuration

### Custom Health Checks

```yaml
backends:
  - id: "api-server"
    url: "http://api.example.com"
    health_check_path: "/api/health"
    timeout: 10s
```

### Backend Weighting

```yaml
backends:
  - id: "powerful-server"
    weight: 3
  - id: "standard-server"
    weight: 1
```

### Circuit Breaker Tuning

```yaml
circuit_breaker:
  failure_threshold: 10     # Failures before opening
  recovery_timeout: 30s     # Time before trying again
  max_requests: 5           # Max requests in half-open state
```
