# Stage 1: Build the Go application
FROM golang:1.21-alpine AS builder

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the application
RUN go build -o gitlab-status

# Stage 2: Create production image
FROM alpine:3.19

WORKDIR /app

# Install dependencies
RUN apk --no-cache add ca-certificates tzdata

# Copy the binary from builder
COPY --from=builder /app/gitlab-status .

# Copy templates and static assets
COPY --from=builder /app/templates ./templates

# Create volume directory for database
RUN mkdir -p /data

# Set environment variables
ENV PORT=8080
ENV DB_PATH=/data/gitlab-status.db

# Expose port
EXPOSE 8080

# Set volume for persistent data
VOLUME ["/data"]

# Run the application
CMD ["./gitlab-status"]