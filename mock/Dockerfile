# Mock "Честный знак" verification service (SPEC §8).
# Build context: mock/. The registry is mounted at runtime, e.g.
#   -v ./data/C16/mock_registry.csv:/data/mock_registry.csv:ro
FROM golang:1.23-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/mock .

FROM alpine:3.20
RUN adduser -D -H -u 10001 app
COPY --from=build /out/mock /usr/local/bin/mock
USER app
ENV ADDR=:8080 \
    REGISTRY_FILE=/data/mock_registry.csv \
    MOCK_MODE=normal \
    MOCK_SLOW_MS=5000
EXPOSE 8080
HEALTHCHECK --interval=5s --timeout=2s --retries=10 \
    CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/usr/local/bin/mock"]
