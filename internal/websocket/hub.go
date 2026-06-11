package websocket

import (
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

type Client struct {
	Hub      *Hub
	Conn     *websocket.Conn
	Send     chan []byte
	UserID   string
	UserName string
	Role     string
	RoomCode string
	RoomID   string
}

type BroadcastMessage struct {
	RoomCode      string
	Data          []byte
	ExcludeUserID string
}

type TargetedMessage struct {
	RoomCode     string
	TargetUserID string
	Data         []byte
}

type PlayerState struct {
	CurrentTime float64
	IsPlaying   bool
	UpdatedAt   time.Time
	UserID      string
	UserName    string
}

type Hub struct {
	mu           sync.RWMutex
	rooms        map[string]map[string]*Client
	playerStates map[string]PlayerState
	register     chan *Client
	unregister   chan *Client
	broadcast    chan BroadcastMessage
	targeted     chan TargetedMessage
}

func NewHub() *Hub {
	return &Hub{
		rooms:        make(map[string]map[string]*Client),
		playerStates: make(map[string]PlayerState),
		register:     make(chan *Client),
		unregister:   make(chan *Client),
		broadcast:    make(chan BroadcastMessage, 256),
		targeted:     make(chan TargetedMessage, 256),
	}
}

func (h *Hub) Run() {
	for {
		select {
		case client := <-h.register:
			h.mu.Lock()
			if h.rooms[client.RoomCode] == nil {
				h.rooms[client.RoomCode] = make(map[string]*Client)
			}
			h.rooms[client.RoomCode][client.UserID] = client
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if clients, ok := h.rooms[client.RoomCode]; ok {
				if registered, exists := clients[client.UserID]; exists && registered == client {
					delete(clients, client.UserID)
					close(client.Send)
					if len(clients) == 0 {
						delete(h.rooms, client.RoomCode)
						delete(h.playerStates, client.RoomCode)
					}
				}
			}
			h.mu.Unlock()

		case msg := <-h.broadcast:
			h.mu.RLock()
			clients := h.rooms[msg.RoomCode]
			for userID, client := range clients {
				if userID == msg.ExcludeUserID {
					continue
				}
				select {
				case client.Send <- msg.Data:
				default:
					close(client.Send)
					delete(clients, userID)
				}
			}
			h.mu.RUnlock()

		case msg := <-h.targeted:
			h.mu.RLock()
			if clients, ok := h.rooms[msg.RoomCode]; ok {
				if client, exists := clients[msg.TargetUserID]; exists {
					select {
					case client.Send <- msg.Data:
					default:
						close(client.Send)
						delete(clients, msg.TargetUserID)
					}
				}
			}
			h.mu.RUnlock()
		}
	}
}

func (h *Hub) Register(client *Client) {
	h.register <- client
}

func (h *Hub) Unregister(client *Client) {
	h.unregister <- client
}

func (h *Hub) IsCurrent(client *Client) bool {
	h.mu.RLock()
	defer h.mu.RUnlock()
	clients := h.rooms[client.RoomCode]
	return clients != nil && clients[client.UserID] == client
}

func (h *Hub) BroadcastToRoom(roomCode string, data []byte, excludeUserID string) {
	h.broadcast <- BroadcastMessage{
		RoomCode:      roomCode,
		Data:          data,
		ExcludeUserID: excludeUserID,
	}
}

func (h *Hub) SendToUser(roomCode string, targetUserID string, data []byte) {
	h.targeted <- TargetedMessage{
		RoomCode:     roomCode,
		TargetUserID: targetUserID,
		Data:         data,
	}
}

func (h *Hub) CountRoomMembers(roomCode string) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if clients, ok := h.rooms[roomCode]; ok {
		return len(clients)
	}
	return 0
}

func (h *Hub) SetPlayerState(roomCode string, state PlayerState) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.playerStates[roomCode] = state
}

func (h *Hub) GetPlayerState(roomCode string) (PlayerState, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	state, ok := h.playerStates[roomCode]
	return state, ok
}
