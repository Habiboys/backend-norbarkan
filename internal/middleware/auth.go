package middleware

import (
	"net/http"
	"strings"

	"backend-nobarkan/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
			c.Abort()
			return
		}

		c.Next()
	}
}
