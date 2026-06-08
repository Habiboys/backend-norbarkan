package router

import (
	"backend-nobarkan/internal/handler"
	"backend-nobarkan/internal/middleware"
	"backend-nobarkan/internal/pkg/response"
	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func New(db *gorm.DB, redisClient *redis.Client) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(middleware.CORS())

	healthHandler := handler.NewHealthHandler(db, redisClient)
	r.GET("/health", healthHandler.Check)

	v1 := r.Group("/v1")
	{
		v1.GET("/ping", func(c *gin.Context) {
			response.OK(c, gin.H{"service": "nobarsync-api"}, "OK")
		})
	}

	return r
}
