#!/bin/bash
docker compose down
docker compose run --rm db-init
docker compose up --build -d
