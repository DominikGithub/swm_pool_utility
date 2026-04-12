FROM golang:1.21-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY *.go ./
COPY go.mod ./
RUN go mod tidy
RUN CGO_ENABLED=1 GOOS=linux go build -o weather-scraper .

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    libsqlite3-0 \
    cron \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /app/weather-scraper .

RUN touch /var/log/weather.log && \
    echo '0 * * * * root /app/weather-scraper --once >> /var/log/weather.log 2>&1' > /etc/cron.d/weather && \
    echo '' >> /etc/cron.d/weather && \
    chmod 0644 /etc/cron.d/weather

CMD /app/weather-scraper --once && cron && tail -f /var/log/weather.log
