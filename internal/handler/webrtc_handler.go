package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"

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
	iceServers := h.staticICEServers()
	if h.cfg.MeteredDomain != "" && h.cfg.MeteredAPIKey != "" {
		meteredServers, err := h.fetchMeteredICEServers(c.Request.Context())
		if err == nil && len(meteredServers) > 0 {
			iceServers = meteredServers
		}
	}

	response.OK(c, gin.H{
		"ice_servers":           iceServers,
		"max_call_participants": h.cfg.MaxCallParticipants,
		"max_participants":      h.cfg.MaxCallParticipants,
	}, "OK")
}

func (h *WebRTCHandler) staticICEServers() []gin.H {
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
	return iceServers
}

func (h *WebRTCHandler) fetchMeteredICEServers(ctx context.Context) ([]gin.H, error) {
	endpoint := fmt.Sprintf(
		"https://%s/api/v1/turn/credentials?apiKey=%s",
		h.cfg.MeteredDomain,
		url.QueryEscape(h.cfg.MeteredAPIKey),
	)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("metered status %d", resp.StatusCode)
	}

	var raw []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	servers := make([]gin.H, 0, len(raw))
	for _, item := range raw {
		server := gin.H{}
		for key, value := range item {
			server[key] = value
		}
		servers = append(servers, server)
	}
	return servers, nil
}
