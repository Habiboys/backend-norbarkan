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
	iceServers := make([]gin.H, 0, 2)

	if len(h.cfg.STUNURLs) > 0 {
		iceServers = append(iceServers, gin.H{"urls": h.cfg.STUNURLs})
	}

	if len(h.cfg.TURNURLs) > 0 && h.cfg.TURNUsername != "" && h.cfg.TURNCredential != "" {
		iceServers = append(iceServers, gin.H{
			"urls":       h.cfg.TURNURLs,
			"username":   h.cfg.TURNUsername,
			"credential": h.cfg.TURNCredential,
		})
	}

	if len(iceServers) == 0 {
		iceServers = append(iceServers, gin.H{"urls": []string{"stun:stun.l.google.com:19302"}})
	}

	response.OK(c, gin.H{
		"ice_servers":           iceServers,
		"max_call_participants": h.cfg.MaxCallParticipants,
		"max_participants":      h.cfg.MaxCallParticipants,
	}, "OK")
}
