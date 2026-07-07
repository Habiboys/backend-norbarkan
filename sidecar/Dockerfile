FROM python:3.12-slim

WORKDIR /app

RUN apt-get update && apt-get install -y --no-install-recommends \
    ffmpeg \
    nodejs \
    && rm -rf /var/lib/apt/lists/*

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY main.py .

EXPOSE 5000

ENV SIDECAR_PORT=5000
ENV STORAGE_PATH=/app/storage/cache/external

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "5000"]
