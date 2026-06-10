package middleware

import (
	"net/http"
	"strings"

	jwtutil "backend-nobarkan/internal/pkg/jwt"
	"backend-nobarkan/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func Auth(accessSecret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" || !strings.HasPrefix(header, "Bearer ") {
			response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
			c.Abort()
			return
		}

		tokenString := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		claims, err := jwtutil.Parse(tokenString, accessSecret)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid atau expired")
			c.Abort()
			return
		}

		c.Set("user_id", claims.UserID)
		c.Set("user_email", claims.Email)
		c.Next()
	}
}
