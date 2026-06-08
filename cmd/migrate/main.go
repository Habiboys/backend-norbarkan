package main

import (
	"flag"
	"log"

	"backend-nobarkan/internal/config"
	"backend-nobarkan/internal/migration"
)

func main() {
	direction := flag.String("direction", "up", "Arah migrasi: up atau down")
	steps := flag.Int("steps", 1, "Jumlah migrasi yang di-rollback saat direction=down")
	migrationsDir := flag.String("dir", "migrations", "Folder file migrasi SQL")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	runner, err := migration.NewRunner(cfg.DB, *migrationsDir)
	if err != nil {
		log.Fatalf("init migration runner: %v", err)
	}
	defer func() { _ = runner.Close() }()

	switch migration.Direction(*direction) {
	case migration.DirectionUp:
		err = runner.Up()
	case migration.DirectionDown:
		err = runner.Down(*steps)
	default:
		log.Fatalf("direction tidak valid: %s", *direction)
	}

	if err != nil {
		log.Fatalf("migration failed: %v", err)
	}
}
