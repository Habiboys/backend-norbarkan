package handler

import (
	"context"
	"time"

	"backend-nobarkan/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

type HealthHandler struct {
	db    *gorm.DB
	redis *redis.Client
}

func NewHealthHandler(db *gorm.DB, redisClient *redis.Client) *HealthHandler {
	return &HealthHandler{db: db, redis: redisClient}
}

func (h *HealthHandler) Check(c *gin.Context) {
	status := gin.H{
		"status":   "ok",
		"database": "connected",
		"redis":    "disabled",
	}

	if h.db == nil {
		status["database"] = "unavailable"
	} else {
		sqlDB, err := h.db.DB()
		if err != nil || sqlDB.Ping() != nil {
			status["database"] = "unavailable"
		} else {
			status["database"] = "connected"
		}
	}

	if h.redis != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := h.redis.Ping(ctx).Err(); err != nil {
			status["redis"] = "unavailable"
		} else {
			status["redis"] = "connected"
		}
	}

	response.OK(c, status, "OK")
}
