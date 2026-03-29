# --- build stage ---
# NOTE: Once pgx replaces internal/pg, bump this to golang:1.25-alpine and
# remove POSTGRES_HOST_AUTH_METHOD=md5 from docker-compose.yml.
FROM golang:1.24-alpine AS builder

WORKDIR /app

# Cache module downloads before copying source.
COPY go.mod ./
RUN go mod download 2>/dev/null || true

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /control-plane ./cmd/control-plane

# --- runtime stage ---
FROM alpine:3.19

RUN apk add --no-cache ca-certificates

WORKDIR /app

COPY --from=builder /control-plane /app/control-plane

# Pre-compiled dashboard assets. Built via: make dashboard
# DASHBOARD_DIR defaults to dashboard/static relative to the working directory.
COPY dashboard/static/ /app/dashboard/static/

EXPOSE 8080

ENTRYPOINT ["/app/control-plane"]
