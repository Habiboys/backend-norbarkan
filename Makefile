.PHONY: tidy run dev test migrate-up migrate-down seed docker-up docker-down \
        sidecar sidecar-docker

tidy:
	go mod tidy

run:
	go run ./cmd/server

dev:
	air

test:
	go test ./...

migrate-up:
	go run ./cmd/migrate -direction up

migrate-down:
	go run ./cmd/migrate -direction down -steps 1

seed:
	go run ./cmd/seed

# Sidecar (yt-dlp Python service)
sidecar:
	cd sidecar && python main.py

sidecar-install:
	pip install -r sidecar/requirements.txt

# Docker
docker-up:
	docker compose up -d mysql redis sidecar

docker-up-all:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f
