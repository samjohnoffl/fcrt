# FCRT Multi-stage Dockerfile
# Builds: fcrt-api, fcrt-worker and migrate (shared image, different entrypoints).
#
#   docker build -t fcrt .
#   docker run fcrt                 # runs the API
#   docker run fcrt /fcrt-worker    # runs the worker
#   docker run fcrt /migrate up     # applies migrations
#
# Local development: see docker-compose.yml.

# ──────────────────────────────────────────────────────────────────────────────
# STAGE 1: Build Go binaries
# ──────────────────────────────────────────────────────────────────────────────
FROM golang:1.24-alpine AS builder

RUN apk add --no-cache git make gcc musl-dev

WORKDIR /src

# Cache dependencies
COPY go.mod go.sum ./
RUN go mod download

# Copy source
COPY . .

# Build binaries
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /fcrt-api    ./cmd/api
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /fcrt-worker ./cmd/worker
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /migrate     ./cmd/migrate

# ──────────────────────────────────────────────────────────────────────────────
# STAGE 2: Runtime (distroless for security)
# ──────────────────────────────────────────────────────────────────────────────
FROM gcr.io/distroless/static-debian12:nonroot

# Copy binaries
COPY --from=builder /fcrt-api    /fcrt-api
COPY --from=builder /fcrt-worker /fcrt-worker
COPY --from=builder /migrate     /migrate

# Copy migrations (used by the compose `migrate` service and the Cloud Run job).
# The migrate binary defaults to MIGRATIONS_DIR=/migrations.
COPY --from=builder /src/migrations /migrations

# Default entrypoint = API server; override with /fcrt-worker for the worker.
ENTRYPOINT ["/fcrt-api"]
EXPOSE 8080
