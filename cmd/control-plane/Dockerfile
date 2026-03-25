# --- build stage ---
FROM golang:1.22-alpine AS builder

WORKDIR /app

# Cache module downloads before copying source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -o /control-plane ./cmd/control-plane

# --- runtime stage ---
FROM alpine:3.19

RUN apk add --no-cache ca-certificates

COPY --from=builder /control-plane /control-plane

EXPOSE 8080

ENTRYPOINT ["/control-plane"]
