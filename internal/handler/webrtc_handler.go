package handler

import (
	"backend-nobarkan/internal/config"
	"backend-nobarkan/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

type WebRTCHandler struct {
	cfg config.WebRTCConfig
}

func NewWebRTCHandler(cfg config.WebRTCConfig) *WebRTCHandler {
	return &WebRTCHandler{cfg: cfg}
}

func (h *WebRTCHandler) Config(c *gin.Context) {
	response.OK(c, gin.H{
		"ice_servers": []gin.H{
			{
				"urls": h.cfg.STUNURLs,
			},
		},
		"max_call_participants": h.cfg.MaxCallParticipants,
	}, "OK")
}
