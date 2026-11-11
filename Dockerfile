# backend/Dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/tunnel-manager ./cmd/server && \
    CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@v3.24.1

FROM alpine:3.20
ARG TARGETARCH
RUN case "$TARGETARCH" in \
      amd64|arm64) cloudflared_arch="$TARGETARCH" ;; \
      *) echo "Unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac && \
    apk add --no-cache ca-certificates curl && \
    curl -fsSL -o /usr/local/bin/cloudflared \
      "https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${cloudflared_arch}" && \
    chmod +x /usr/local/bin/cloudflared
COPY --from=builder /out/tunnel-manager /usr/local/bin/tunnel-manager
COPY --from=builder /go/bin/goose /usr/local/bin/goose
COPY migrations /app/migrations
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
