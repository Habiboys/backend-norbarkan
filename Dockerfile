FROM golang:1.25-alpine AS builder

WORKDIR /app

RUN apk add --no-cache git

COPY go.mod go.sum* ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/nobarsync-api ./cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o /bin/nobarsync-migrate ./cmd/migrate

FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache ca-certificates ffmpeg

COPY --from=builder /bin/nobarsync-api /usr/local/bin/nobarsync-api
COPY --from=builder /bin/nobarsync-migrate /usr/local/bin/nobarsync-migrate
COPY migrations ./migrations
COPY .env.example ./.env.example

RUN mkdir -p /app/storage/originals /app/storage/hls /app/storage/cache/external

EXPOSE 8080

CMD ["nobarsync-api"]
