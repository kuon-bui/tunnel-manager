# backend/Dockerfile
FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/tunnel-manager ./cmd/server

FROM alpine:3.20
RUN apk add --no-cache ca-certificates curl && \
    curl -fsSL -o /usr/local/bin/cloudflared \
      https://github.com/cloudflare/cloudflared/releases/latest/download/cloudflared-linux-amd64 && \
    chmod +x /usr/local/bin/cloudflared
COPY --from=builder /out/tunnel-manager /usr/local/bin/tunnel-manager
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/tunnel-manager"]
