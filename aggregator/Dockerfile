FROM golang:1.21-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY *.go ./
COPY go.mod ./
RUN go mod tidy
RUN CGO_ENABLED=1 GOOS=linux go build -o aggregator .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libsqlite3-0 \
    cron \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/aggregator .

RUN touch /var/log/aggregator.log && \
    echo '0 * * * * root /app/aggregator --once >> /var/log/aggregator.log 2>&1' > /etc/cron.d/aggregator && \
    echo '' >> /etc/cron.d/aggregator && \
    chmod 0644 /etc/cron.d/aggregator

CMD cron && tail -f /var/log/aggregator.log
