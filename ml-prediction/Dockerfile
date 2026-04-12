FROM python:3.12-slim

WORKDIR /app

RUN apt-get update && apt-get install --no-install-recommends -y \
    gcc \
    libsqlite3-dev \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY predict.py .

ENV MODEL_DIR=/models
ENV DB_PATH=/data/swm_pool_utility.db

CMD ["python", "predict.py", "--daemon"]
