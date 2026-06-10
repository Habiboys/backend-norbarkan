.PHONY: tidy run dev test migrate-up migrate-down seed docker-up docker-down

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

docker-up:
	docker compose up -d mysql redis

docker-down:
	docker compose down
