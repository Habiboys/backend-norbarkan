package main

import (
	"context"
	"fmt"
	"log"

	"backend-nobarkan/internal/config"
	"backend-nobarkan/internal/database"
	"backend-nobarkan/internal/router"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	logger, err := zap.NewProduction()
	if cfg.App.Env == "development" {
		logger, err = zap.NewDevelopment()
	}
	if err != nil {
		log.Fatalf("init logger: %v", err)
	}
	defer func() { _ = logger.Sync() }()

	ctx := context.Background()

	db, err := database.NewMySQL(cfg.DB)
	if err != nil {
		logger.Fatal("failed to connect database", zap.Error(err))
	}

	redisClient, err := database.NewRedis(ctx, cfg.Redis)
	if err != nil {
		logger.Warn("redis unavailable, server will start without redis", zap.Error(err))
		redisClient = nil
	}
	if redisClient != nil {
		defer func() { _ = redisClient.Close() }()
	}

	app := router.New(db, redisClient, cfg)
	addr := fmt.Sprintf(":%s", cfg.App.Port)

	logger.Info("server started", zap.String("addr", addr), zap.String("env", cfg.App.Env))
	if err := app.Run(addr); err != nil {
		logger.Fatal("server stopped", zap.Error(err))
	}
}
