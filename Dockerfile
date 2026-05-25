# syntax=docker/dockerfile:1.7

FROM golang:1.25-alpine AS builder
WORKDIR /src
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux \
    go build -trimpath -ldflags="-s -w" -o /out/kkapi .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata curl su-exec \
 && addgroup -S app && adduser -S app -G app
WORKDIR /app
COPY --from=builder /out/kkapi /app/kkapi
RUN mkdir -p /app/uploads && chown -R app:app /app
ENV PORT=8080 \
    UPLOAD_DIR=/app/uploads
EXPOSE 8080
# Start as root so we can chown the mounted /app/uploads volume
# (Coolify mounts the volume as root and the image's chown is hidden),
# then drop to the unprivileged 'app' user via su-exec.
ENTRYPOINT ["/bin/sh", "-c", "chown -R app:app /app/uploads && exec su-exec app /app/kkapi"]
