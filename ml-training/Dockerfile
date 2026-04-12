FROM python:3.12-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    gcc \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY train.py .

ENV MODEL_DIR=/models
ENV DB_PATH=/data/swm_pool_utility.db

ENTRYPOINT ["python", "train.py"]
