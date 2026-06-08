# Backend Nobarkan

Backend Go untuk NobarSync/Nobarkan. Project ini memakai Gin, GORM, MySQL, Redis, migrasi SQL manual runner, dan seeder awal.

## Quick Start

Jalankan urutan command berikut untuk setup project dari awal:

```powershell
cd D:\Nouval\norbarkan\backend-nobarkan
Copy-Item .env.example .env
docker compose up -d mysql redis
go mod tidy
go run ./cmd/migrate -direction up
go run ./cmd/seed
go run ./cmd/server
```

Setelah server berjalan, cek API:

```powershell
Invoke-RestMethod http://localhost:8080/health
Invoke-RestMethod http://localhost:8080/v1/ping
```

## Prasyarat

- Go terinstall. Disarankan Go versi 1.22 atau lebih baru.
- Docker Desktop aktif jika ingin menjalankan MySQL dan Redis via Docker Compose.
- Git opsional, hanya diperlukan jika clone project dari repository.
- Jalankan semua command dari folder `backend-nobarkan`.

```powershell
cd D:\Nouval\norbarkan\backend-nobarkan
```

## Konsep Dependency di Go

Kalau di Node.js biasanya pakai:

```powershell
npm install
```

Di Go, dependency project dibaca dari `go.mod` dan `go.sum`. Command yang setara untuk download dan merapikan dependency adalah:

```powershell
go mod tidy
```

Command tersebut akan:

- download package yang dipakai project,
- membersihkan dependency yang tidak dipakai,
- memperbarui `go.sum`,
- memastikan import di kode sesuai dengan isi `go.mod`.

Jika hanya ingin download dependency tanpa merapikan:

```powershell
go mod download
```

Untuk menambahkan package baru, gunakan:

```powershell
go get nama/package
```

Contoh:

```powershell
go get github.com/gin-gonic/gin
```

Setelah menambah package, jalankan lagi:

```powershell
go mod tidy
```

## Setup Environment

Salin file contoh environment menjadi `.env`.

```powershell
Copy-Item .env.example .env
```

Jika menjalankan backend langsung dari host Windows, biarkan nilai ini:

```env
DB_HOST=localhost
REDIS_HOST=localhost
```

Jika menjalankan backend di container Docker Compose, gunakan nilai service Docker:

```env
DB_HOST=mysql
REDIS_HOST=redis
```

## Menjalankan MySQL dan Redis

Untuk menjalankan MySQL dan Redis saja:

```powershell
docker compose up -d mysql redis
```

Untuk melihat status container:

```powershell
docker compose ps
```

## Install Dependency

Jalankan command berikut dari folder `backend-nobarkan`:

```powershell
go mod tidy
```

Jika dependency belum pernah didownload, Go akan otomatis mendownload semua package yang dibutuhkan.

Alternatif jika hanya ingin download dependency:

```powershell
go mod download
```

Untuk memastikan project bisa dikompilasi setelah install dependency:

```powershell
go test ./...
```

## Migrasi Database

Migrasi memakai command internal `cmd/migrate` dan membaca file SQL dari folder `migrations`.

### Jalankan semua migrasi up

```powershell
go run ./cmd/migrate -direction up
```

### Rollback migrasi terakhir

```powershell
go run ./cmd/migrate -direction down -steps 1
```

### Rollback beberapa migrasi

```powershell
go run ./cmd/migrate -direction down -steps 3
```

### Pakai folder migrasi custom

```powershell
go run ./cmd/migrate -direction up -dir migrations
```

Runner migrasi akan membuat tabel `schema_migrations` untuk mencatat migrasi yang sudah dijalankan.

## Seeder

Seeder awal membuat user demo berikut jika belum ada:

- `admin@nobarsync.local` / `password123`
- `nouval@example.com` / `password123`

Pastikan migrasi sudah dijalankan lebih dulu.

```powershell
go run ./cmd/seed
```

Seeder bersifat idempotent untuk user berdasarkan email. Jika user sudah ada, seeder akan skip.

## Menjalankan Backend

Pastikan MySQL dan Redis aktif, migrasi sudah dijalankan, lalu jalankan server:

```powershell
go run ./cmd/server
```

Server berjalan di port dari `.env`, default:

```text
http://localhost:8080
```

Cek health endpoint:

```powershell
Invoke-RestMethod http://localhost:8080/health
```

Cek endpoint ping API:

```powershell
Invoke-RestMethod http://localhost:8080/v1/ping
```

## Shortcut Makefile

Jika `make` tersedia di sistem, bisa pakai command berikut:

```powershell
make tidy
make migrate-up
make migrate-down
make seed
make run
make test
```

Di Windows tanpa `make`, gunakan command `go run ...` yang sudah ditulis di atas.

## Menjalankan dengan Docker Compose

Build dan jalankan semua service:

```powershell
docker compose up --build
```

Catatan: migrasi dan seeder tetap dijalankan sebagai command terpisah dari host atau bisa dieksekusi di container backend jika container sudah berjalan.

Dari host:

```powershell
go run ./cmd/migrate -direction up
go run ./cmd/seed
```

Jika ingin eksekusi dari container backend:

```powershell
docker compose run --rm backend nobarsync-api
```

Untuk saat ini image backend hanya menjalankan server. Command migrasi/seeder direkomendasikan dijalankan dari host dengan Go.

## Urutan Setup yang Disarankan

```powershell
cd D:\Nouval\norbarkan\backend-nobarkan
Copy-Item .env.example .env
docker compose up -d mysql redis
go mod tidy
go run ./cmd/migrate -direction up
go run ./cmd/seed
go run ./cmd/server
```

## Troubleshooting

### Error koneksi MySQL

Pastikan container MySQL sudah healthy:

```powershell
docker compose ps
```

Pastikan `.env` memakai:

```env
DB_HOST=localhost
DB_PORT=3306
DB_USER=root
DB_PASS=password
DB_NAME=nobarsync
```

### Error koneksi Redis

Pastikan Redis aktif:

```powershell
docker compose ps redis
```

Pastikan `.env` memakai:

```env
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASS=
```

### Error `go.mod` dibaca sebagai XML

Ubah language mode file `go.mod` di editor menjadi `Go Module` atau `Go Mod`.
