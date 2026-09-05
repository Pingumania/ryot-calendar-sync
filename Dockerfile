# syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS builder
WORKDIR /src

# No third-party dependencies (stdlib only), so there's no go.sum to copy or
# verify -- this stays a single, reproducible layer.
COPY go.mod ./
COPY cmd ./cmd
COPY internal ./internal

RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/ryot-calendar-sync ./cmd/ryot-calendar-sync

FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates \
    && addgroup -S app && adduser -S -G app app
USER app

COPY --from=builder /out/ryot-calendar-sync /usr/local/bin/ryot-calendar-sync

EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -qO- "http://127.0.0.1:8090/healthz" || exit 1

ENTRYPOINT ["ryot-calendar-sync"]
