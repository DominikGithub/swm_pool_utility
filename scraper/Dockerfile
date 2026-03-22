FROM golang:1.21-bookworm AS builder

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY *.go ./
COPY go.mod ./
RUN go mod tidy
RUN CGO_ENABLED=1 GOOS=linux go build -o scraper

FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    chromium \
    chromium-sandbox \
    libsqlite3-0 \
    ca-certificates \
    fonts-noto \
    cron \
    && rm -rf /var/lib/apt/lists/*

ENV CHROME_BIN=/usr/bin/chromium
COPY --from=builder /app/scraper /scraper

RUN touch /var/log/scraper.log && \
    echo '*/10 * * * * root /scraper --once >> /var/log/scraper.log 2>&1' > /etc/cron.d/scraper && \
    echo '' >> /etc/cron.d/scraper && \
    chmod 0644 /etc/cron.d/scraper

CMD cron && tail -f /var/log/scraper.log
