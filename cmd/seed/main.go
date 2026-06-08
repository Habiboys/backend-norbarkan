package main

import (
	"log"

	"backend-nobarkan/internal/config"
	"backend-nobarkan/internal/database"
	"backend-nobarkan/internal/seeder"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := database.NewMySQL(cfg.DB)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}

	runner := seeder.New(db)
	if err := runner.Run(); err != nil {
		log.Fatalf("seed failed: %v", err)
	}

	log.Println("seed completed")
}
