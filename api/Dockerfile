FROM golang:1.21-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=1 GOOS=linux go build -o api .

FROM debian:12.9-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libsqlite3-0 \
    wget \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /data && chown -R 65534:65534 /data
USER 65534
WORKDIR /data
COPY --from=builder /app/api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
