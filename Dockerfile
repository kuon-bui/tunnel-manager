FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/tunnel-manager ./cmd/server && \
    CGO_ENABLED=0 go install github.com/pressly/goose/v3/cmd/goose@v3.24.1

FROM alpine:3.20
ARG TARGETARCH
# "latest" takes the newest release at build time; pass a version such as
# 2026.11.0 to pin it instead
ARG CLOUDFLARED_VERSION=latest
# cloudflared updates itself by replacing the binary in its own directory, so
# /opt/cloudflared belongs to the runtime user.
# The UID/GID match the `deploy` user on the server so bind-mounted data stays
# owned by it
ARG APP_UID=10001
ARG APP_GID=10001
RUN case "$TARGETARCH" in \
      amd64|arm64) cloudflared_arch="$TARGETARCH" ;; \
      *) echo "Unsupported architecture: $TARGETARCH" >&2; exit 1 ;; \
    esac && \
    if [ "$CLOUDFLARED_VERSION" = latest ]; then \
      cloudflared_url="https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-${cloudflared_arch}"; \
    else \
      cloudflared_url="https://github.com/cloudflare/cloudflared/releases/download/${CLOUDFLARED_VERSION}/cloudflared-linux-${cloudflared_arch}"; \
    fi && \
    apk add --no-cache ca-certificates curl && \
    addgroup -S -g "$APP_GID" app && \
    adduser -S -D -H -u "$APP_UID" -G app app && \
    mkdir -p /opt/cloudflared /data && \
    curl -fsSL -o /opt/cloudflared/cloudflared "$cloudflared_url" && \
    chmod +x /opt/cloudflared/cloudflared && \
    ln -s /opt/cloudflared/cloudflared /usr/local/bin/cloudflared && \
    chown -R app:app /opt/cloudflared /data
COPY --from=builder /out/tunnel-manager /usr/local/bin/tunnel-manager
COPY --from=builder /go/bin/goose /usr/local/bin/goose
COPY migrations /app/migrations
COPY docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
ENV CLOUDFLARED_BINARY=/opt/cloudflared/cloudflared
USER app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
