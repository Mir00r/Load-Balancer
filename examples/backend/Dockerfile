# Build stage
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY examples/backend/main.go .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -o backend main.go

# Final stage
FROM alpine:latest
RUN apk --no-cache add ca-certificates wget
WORKDIR /app
COPY --from=builder /app/backend .
EXPOSE 8081
CMD ["./backend", "8081"]
