FROM golang:1.21-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY *.go ./
COPY go.mod ./
RUN go mod tidy
RUN CGO_ENABLED=1 GOOS=linux go build -o daily-stats .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libsqlite3-0 \
    cron \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/daily-stats .

RUN touch /var/log/daily-stats.log && \
    echo '0 * * * * root /app/daily-stats --once >> /var/log/daily-stats.log 2>&1' > /etc/cron.d/daily-stats && \
    echo '' >> /etc/cron.d/daily-stats && \
    chmod 0644 /etc/cron.d/daily-stats

CMD /app/daily-stats --once && cron && tail -f /var/log/daily-stats.log
