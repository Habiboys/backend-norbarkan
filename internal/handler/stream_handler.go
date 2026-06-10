package handler

import (
	"net/http"
	"path/filepath"
	"strings"

	"backend-nobarkan/internal/config"
	jwtutil "backend-nobarkan/internal/pkg/jwt"
	"backend-nobarkan/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type StreamHandler struct {
	storage   config.StorageConfig
	jwtConfig config.JWTConfig
}

func NewStreamHandler(storage config.StorageConfig, jwtConfig config.JWTConfig) *StreamHandler {
	return &StreamHandler{storage: storage, jwtConfig: jwtConfig}
}

func (h *StreamHandler) Master(c *gin.Context) {
	if !h.authorized(c) {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}
	path := filepath.Join(h.storage.Path, "hls", c.Param("movie_id"), "master.m3u8")
	c.File(path)
}

func (h *StreamHandler) Segment(c *gin.Context) {
	if !h.authorized(c) {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}
	segment := filepath.Base(c.Param("segment"))
	path := filepath.Join(h.storage.Path, "hls", c.Param("movie_id"), segment)
	c.File(path)
}

func (h *StreamHandler) authorized(c *gin.Context) bool {
	token := c.Query("token")
	if token == "" {
		header := c.GetHeader("Authorization")
		if strings.HasPrefix(header, "Bearer ") {
			token = strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
		}
	}
	if token == "" {
		return false
	}
	_, err := jwtutil.Parse(token, h.jwtConfig.AccessSecret)
	return err == nil
}
