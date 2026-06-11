package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
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

var cachedICEServers []gin.H
var cachedICEServersMu sync.RWMutex
var cachedICEServersAt time.Time

func (h *WebRTCHandler) Config(c *gin.Context) {
	iceServers := h.staticICEServers()

	if h.cfg.MeteredDomain != "" && h.cfg.MeteredAPIKey != "" {
		meteredServers := h.getCachedMetered(c.Request.Context())
		if len(meteredServers) > 0 {
			// Combine: static (STUN fallback) + TURN from Metered
			combined := make([]gin.H, 0, len(iceServers)+len(meteredServers))
			combined = append(combined, iceServers...)
			combined = append(combined, meteredServers...)
			iceServers = combined
		}
	}

	response.OK(c, gin.H{
		"ice_servers":           iceServers,
		"max_call_participants": h.cfg.MaxCallParticipants,
		"max_participants":      h.cfg.MaxCallParticipants,
	}, "OK")
}

func (h *WebRTCHandler) getCachedMetered(ctx context.Context) []gin.H {
	cachedICEServersMu.RLock()
	if time.Since(cachedICEServersAt) < 5*time.Minute {
		defer cachedICEServersMu.RUnlock()
		return cachedICEServers
	}
	cachedICEServersMu.RUnlock()

	cachedICEServersMu.Lock()
	defer cachedICEServersMu.Unlock()

	// Double-check after acquiring write lock
	if time.Since(cachedICEServersAt) < 5*time.Minute {
		return cachedICEServers
	}

	servers, err := h.fetchMeteredICEServers(ctx)
	if err != nil {
		return cachedICEServers // keep stale cache on error
	}
	cachedICEServers = servers
	cachedICEServersAt = time.Now()
	return servers
}
func (h *WebRTCHandler) staticICEServers() []gin.H {
	if len(h.cfg.STUNURLs) > 0 {
		return []gin.H{{"urls": h.cfg.STUNURLs}}
	}
	return []gin.H{}
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
