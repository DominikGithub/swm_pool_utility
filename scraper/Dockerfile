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
    && rm -rf /var/lib/apt/lists/*

ENV CHROME_BIN=/usr/bin/chromium
COPY --from=builder /app/scraper /scraper
ENTRYPOINT ["/scraper"]
