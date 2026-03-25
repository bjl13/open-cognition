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

COPY --from=builder /control-plane /control-plane

EXPOSE 8080

ENTRYPOINT ["/control-plane"]
