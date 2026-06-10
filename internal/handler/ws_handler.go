package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"time"

	"backend-nobarkan/internal/domain"
	jwtutil "backend-nobarkan/internal/pkg/jwt"
	"backend-nobarkan/internal/repository"
	"backend-nobarkan/internal/websocket"

	gojwt "github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	gorillaWs "github.com/gorilla/websocket"
)

var upgrader = gorillaWs.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type ChatSendPayload struct {
	Message string `json:"message"`
}

type PlayerPayload struct {
	CurrentTime float64 `json:"current_time"`
	IsPlaying   bool    `json:"is_playing"`
	SentAt      int64   `json:"sent_at"`
}

type WebRTCPayload struct {
	TargetUserID string `json:"target_user_id"`
	SDP          string `json:"sdp,omitempty"`
	Candidate    string `json:"candidate,omitempty"`
}

type WSHandler struct {
	hub           *websocket.Hub
	chatRepo      *repository.ChatRepository
	userRepo      *repository.UserRepository
	roomRepo      *repository.RoomRepository
	memberRepo    *repository.RoomMemberRepository
	jwtAccessSect string
}

func NewWSHandler(hub *websocket.Hub, chatRepo *repository.ChatRepository, userRepo *repository.UserRepository, roomRepo *repository.RoomRepository, memberRepo *repository.RoomMemberRepository, jwtAccessSecret string) *WSHandler {
	return &WSHandler{
		hub:           hub,
		chatRepo:      chatRepo,
		userRepo:      userRepo,
		roomRepo:      roomRepo,
		memberRepo:    memberRepo,
		jwtAccessSect: jwtAccessSecret,
	}
}

func (h *WSHandler) Serve(w http.ResponseWriter, r *http.Request) {
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		http.Error(w, "token required", http.StatusUnauthorized)
		return
	}

	claims := &jwtutil.Claims{}
	token, err := gojwt.ParseWithClaims(tokenStr, claims, func(token *gojwt.Token) (interface{}, error) {
		return []byte(h.jwtAccessSect), nil
	})
	if err != nil || !token.Valid {
		http.Error(w, "invalid token", http.StatusUnauthorized)
		return
	}

	userID := claims.UserID
	roomCode := claims.Email

	user, err := h.userRepo.FindByID(userID)
	if err != nil || user == nil {
		http.Error(w, "user not found", http.StatusUnauthorized)
		return
	}

	room, err := h.roomRepo.FindByCode(roomCode)
	if err != nil || room == nil {
		http.Error(w, "room not found", http.StatusUnauthorized)
		return
	}

	userName := user.Name

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("ws upgrade error: %v", err)
		return
	}

	role := "member"
	if room.HostID == userID {
		role = "host"
	}

	client := &websocket.Client{
		Hub:      h.hub,
		Conn:     conn,
		Send:     make(chan []byte, 256),
		UserID:   userID,
		UserName: userName,
		Role:     role,
		RoomCode: roomCode,
		RoomID:   room.ID,
	}

	h.hub.Register(client)

	joinPayload, _ := json.Marshal(map[string]interface{}{
		"type": "member:join",
		"payload": map[string]string{
			"id":   userID,
			"name": userName,
			"role": role,
		},
	})
	h.hub.BroadcastToRoom(roomCode, joinPayload, userID)

	if role != "host" {
		h.sendPlayerSnapshotToClient(client)
	}

	go h.writePump(client)
	go h.readPump(client)
}

func (h *WSHandler) readPump(client *websocket.Client) {
	client.Conn.SetReadLimit(64 * 1024)
	client.Conn.SetReadDeadline(time.Now().Add(70 * time.Second))
	client.Conn.SetPongHandler(func(string) error {
		client.Conn.SetReadDeadline(time.Now().Add(70 * time.Second))
		return nil
	})

	defer func() {
		h.hub.Unregister(client)
		if err := h.memberRepo.Leave(client.RoomID, client.UserID); err != nil {
			log.Printf("room member leave error: %v", err)
		}

		leavePayload, _ := json.Marshal(map[string]interface{}{
			"type": "member:leave",
			"payload": map[string]string{
				"id":   client.UserID,
				"name": client.UserName,
				"role": client.Role,
			},
		})
		h.hub.BroadcastToRoom(client.RoomCode, leavePayload, "")

		client.Conn.Close()
	}()

	for {
		_, message, err := client.Conn.ReadMessage()
		if err != nil {
			if gorillaWs.IsUnexpectedCloseError(err, gorillaWs.CloseGoingAway, gorillaWs.CloseNormalClosure) {
				log.Printf("ws read error: %v", err)
			}
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			continue
		}

		h.dispatchMessage(client, &msg)
	}
}

func (h *WSHandler) writePump(client *websocket.Client) {
	ticker := time.NewTicker(30 * time.Second)
	defer func() {
		ticker.Stop()
		client.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-client.Send:
			if !ok {
				client.Conn.WriteMessage(gorillaWs.CloseMessage, []byte{})
				return
			}
			if err := client.Conn.WriteMessage(gorillaWs.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			if err := client.Conn.WriteMessage(gorillaWs.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *WSHandler) dispatchMessage(client *websocket.Client, msg *WSMessage) {
	switch msg.Type {
	case "chat:send":
		h.handleChatSend(client, msg.Payload)
	case "player:play", "player:pause", "player:seek":
		h.handlePlayerSync(client, msg.Type, msg.Payload)
	case "player:request_sync":
		h.handlePlayerSyncRequest(client)
	case "webrtc:offer", "webrtc:answer", "webrtc:ice":
		h.handleWebRTC(client, msg.Type, msg.Payload)
	case "webrtc:start", "webrtc:stop":
		h.handleWebRTCStartStop(client, msg.Type)
	}
}

func (h *WSHandler) handleChatSend(client *websocket.Client, raw json.RawMessage) {
	var payload ChatSendPayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.Message == "" {
		return
	}

	chat := &domain.Chat{
		ID:        uuid.NewString(),
		RoomID:    client.RoomID,
		UserID:    client.UserID,
		Message:   payload.Message,
		Type:      domain.ChatTypeText,
		CreatedAt: time.Now(),
	}
	if err := h.chatRepo.Create(chat); err != nil {
		log.Printf("chat save error: %v", err)
		return
	}

	respPayload, _ := json.Marshal(map[string]interface{}{
		"type": "chat:new",
		"payload": map[string]interface{}{
			"id":      chat.ID,
			"user_id": client.UserID,
			"user": map[string]string{
				"id":   client.UserID,
				"name": client.UserName,
			},
			"message":    chat.Message,
			"created_at": chat.CreatedAt,
		},
	})
	h.hub.BroadcastToRoom(client.RoomCode, respPayload, "")
}

func (h *WSHandler) handlePlayerSync(client *websocket.Client, eventType string, raw json.RawMessage) {
	// Only host can control playback
	room, err := h.roomRepo.FindByCode(client.RoomCode)
	if err != nil || room == nil || room.HostID != client.UserID || room.Status == domain.RoomStatusEnded {
		return
	}

	var payload PlayerPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return
	}

	isPlaying := eventType == "player:play"
	if eventType == "player:seek" {
		isPlaying = payload.IsPlaying
	}

	room.CurrentTime = payload.CurrentTime
	room.IsPlaying = isPlaying
	if isPlaying {
		room.Status = domain.RoomStatusPlaying
		if room.StartedAt == nil {
			now := time.Now()
			room.StartedAt = &now
		}
	} else if room.Status == domain.RoomStatusPlaying || room.Status == domain.RoomStatusWaiting {
		room.Status = domain.RoomStatusPaused
	}
	if err := h.roomRepo.Update(room); err != nil {
		log.Printf("room player state update error: %v", err)
	}

	state := websocket.PlayerState{
		CurrentTime: payload.CurrentTime,
		IsPlaying:   isPlaying,
		UpdatedAt:   time.Now(),
		UserID:      client.UserID,
		UserName:    client.UserName,
	}
	h.hub.SetPlayerState(client.RoomCode, state)

	respPayload, _ := json.Marshal(map[string]interface{}{
		"type": "player:sync",
		"payload": map[string]interface{}{
			"current_time": payload.CurrentTime,
			"is_playing":   isPlaying,
			"room_status":  room.Status,
			"sent_at":      payload.SentAt,
			"user_id":      client.UserID,
			"user_name":    client.UserName,
		},
	})
	h.hub.BroadcastToRoom(client.RoomCode, respPayload, client.UserID)
}

func (h *WSHandler) sendPlayerSnapshotToClient(client *websocket.Client) {
	state, ok := h.hub.GetPlayerState(client.RoomCode)
	room, _ := h.roomRepo.FindByCode(client.RoomCode)
	roomStatus := domain.RoomStatusWaiting
	currentTime := 0.0
	isPlaying := false
	userID := ""
	userName := ""

	if room != nil {
		roomStatus = room.Status
		currentTime = room.CurrentTime
		isPlaying = room.IsPlaying
	}

	if ok {
		currentTime = state.CurrentTime
		isPlaying = state.IsPlaying
		userID = state.UserID
		userName = state.UserName
		if state.IsPlaying {
			currentTime += time.Since(state.UpdatedAt).Seconds()
		}
	} else if room == nil {
		return
	}

	payload, _ := json.Marshal(map[string]interface{}{
		"type": "player:sync",
		"payload": map[string]interface{}{
			"current_time": currentTime,
			"is_playing":   isPlaying,
			"room_status":  roomStatus,
			"sent_at":      time.Now().UnixMilli(),
			"user_id":      userID,
			"user_name":    userName,
		},
	})

	select {
	case client.Send <- payload:
	default:
	}
}

func (h *WSHandler) handlePlayerSyncRequest(client *websocket.Client) {
	if client.Role != "host" {
		h.sendPlayerSnapshotToClient(client)
		return
	}

	state, ok := h.hub.GetPlayerState(client.RoomCode)
	if ok && state.IsPlaying {
		state.CurrentTime += time.Since(state.UpdatedAt).Seconds()
		state.UpdatedAt = time.Now()
		h.hub.SetPlayerState(client.RoomCode, state)
	}
}

func (h *WSHandler) handleWebRTC(client *websocket.Client, eventType string, raw json.RawMessage) {
	var payload WebRTCPayload
	if err := json.Unmarshal(raw, &payload); err != nil || payload.TargetUserID == "" {
		return
	}

	var webrtcType string
	switch eventType {
	case "webrtc:offer":
		webrtcType = "webrtc:offer"
	case "webrtc:answer":
		webrtcType = "webrtc:answer"
	case "webrtc:ice":
		webrtcType = "webrtc:ice"
	}

	respPayload, _ := json.Marshal(map[string]interface{}{
		"type": webrtcType,
		"payload": map[string]interface{}{
			"sender_user_id": client.UserID,
			"sender_name":    client.UserName,
			"sdp":            payload.SDP,
			"candidate":      payload.Candidate,
		},
	})
	h.hub.SendToUser(client.RoomCode, payload.TargetUserID, respPayload)
}

func (h *WSHandler) handleWebRTCStartStop(client *websocket.Client, eventType string) {
	payload, _ := json.Marshal(map[string]interface{}{
		"type": eventType,
		"payload": map[string]string{
			"user_id":   client.UserID,
			"user_name": client.UserName,
			"role":      client.Role,
		},
	})
	h.hub.BroadcastToRoom(client.RoomCode, payload, client.UserID)
}
