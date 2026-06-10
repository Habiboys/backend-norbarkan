package handler

import (
	"errors"
	"net/http"
	"strconv"
	"time"

	"backend-nobarkan/internal/pkg/response"
	"backend-nobarkan/internal/repository"
	"backend-nobarkan/internal/service"
	"github.com/gin-gonic/gin"
)

type RoomHandler struct {
	rooms *service.RoomService
}

type createRoomRequest struct {
	Name       string  `json:"name" binding:"required"`
	MovieID    *string `json:"movie_id"`
	Mode       string  `json:"mode" binding:"required"`
	IsPrivate  bool    `json:"is_private"`
	Password   *string `json:"password"`
	MaxMembers uint    `json:"max_members"`
}

type joinRoomRequest struct {
	Password *string `json:"password"`
}

type updateRoomRequest struct {
	Name       *string `json:"name"`
	IsPrivate  *bool   `json:"is_private"`
	Password   *string `json:"password"`
	MaxMembers *uint   `json:"max_members"`
}

type sendChatRequest struct {
	Message string `json:"message" binding:"required"`
}

func NewRoomHandler(rooms *service.RoomService) *RoomHandler {
	return &RoomHandler{rooms: rooms}
}

func (h *RoomHandler) Create(c *gin.Context) {
	userID := currentUserID(c)
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}
	var req createRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "Payload room tidak valid")
		return
	}
	room, err := h.rooms.Create(service.CreateRoomInput{Name: req.Name, MovieID: req.MovieID, Mode: req.Mode, IsPrivate: req.IsPrivate, Password: req.Password, MaxMembers: req.MaxMembers, HostID: userID})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal membuat room")
		return
	}
	response.Created(c, room, "Room berhasil dibuat")
}

func (h *RoomHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	rooms, total, page, perPage, err := h.rooms.List(repository.RoomListFilter{Page: page, PerPage: perPage, Status: c.Query("status")})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal mengambil data room")
		return
	}
	response.WithMeta(c, http.StatusOK, rooms, gin.H{"page": page, "per_page": perPage, "total": total}, "OK")
}

func (h *RoomHandler) GetByCode(c *gin.Context) {
	room, err := h.rooms.GetByCode(c.Param("room"))
	if err != nil {
		if errors.Is(err, service.ErrRoomNotFound) {
			response.Error(c, http.StatusNotFound, "ROOM_NOT_FOUND", "Room tidak ditemukan")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal mengambil detail room")
		return
	}
	response.OK(c, room, "OK")
}

func (h *RoomHandler) Join(c *gin.Context) {
	userID := currentUserID(c)
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}
	var req joinRoomRequest
	_ = c.ShouldBindJSON(&req)
	result, err := h.rooms.Join(c.Param("room"), userID, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrRoomNotFound) {
			response.Error(c, http.StatusNotFound, "ROOM_NOT_FOUND", "Room tidak ditemukan")
			return
		}
		if errors.Is(err, service.ErrRoomWrongPassword) {
			response.Error(c, http.StatusForbidden, "ROOM_WRONG_PASSWORD", "Password room salah")
			return
		}
		if errors.Is(err, service.ErrRoomFull) {
			response.Error(c, http.StatusConflict, "ROOM_FULL", "Room sudah penuh")
			return
		}
		if errors.Is(err, service.ErrRoomEnded) {
			response.Error(c, http.StatusGone, "ROOM_ENDED", "Room sudah berakhir dan tidak bisa dijoin lagi")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal join room")
		return
	}
	response.OK(c, result, "Berhasil join room")
}

func (h *RoomHandler) Leave(c *gin.Context) {
	userID := currentUserID(c)
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}
	if err := h.rooms.Leave(c.Param("room"), userID); err != nil {
		if errors.Is(err, service.ErrRoomNotFound) {
			response.Error(c, http.StatusNotFound, "ROOM_NOT_FOUND", "Room tidak ditemukan")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal leave room")
		return
	}
	response.OK(c, nil, "Berhasil keluar dari room")
}

func (h *RoomHandler) Close(c *gin.Context) {
	if err := h.rooms.Close(c.Param("room"), currentUserID(c)); err != nil {
		h.handleRoomError(c, err, "Gagal menutup room")
		return
	}
	response.OK(c, nil, "Room berhasil ditutup")
}

func (h *RoomHandler) Delete(c *gin.Context) {
	if err := h.rooms.Delete(c.Param("room"), currentUserID(c)); err != nil {
		h.handleRoomError(c, err, "Gagal menghapus room")
		return
	}
	response.OK(c, nil, "Room berhasil dihapus")
}

func (h *RoomHandler) Update(c *gin.Context) {
	var req updateRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "Payload update room tidak valid")
		return
	}
	room, err := h.rooms.Update(c.Param("room"), currentUserID(c), service.UpdateRoomInput{Name: req.Name, IsPrivate: req.IsPrivate, Password: req.Password, MaxMembers: req.MaxMembers})
	if err != nil {
		h.handleRoomError(c, err, "Gagal update room")
		return
	}
	response.OK(c, room, "Room berhasil diupdate")
}

func (h *RoomHandler) MyRooms(c *gin.Context) {
	rooms, err := h.rooms.MyRooms(currentUserID(c))
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal mengambil room user")
		return
	}
	response.OK(c, rooms, "OK")
}

func (h *RoomHandler) SendChat(c *gin.Context) {
	userID := currentUserID(c)
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}
	var req sendChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "Pesan chat wajib diisi")
		return
	}
	chat, err := h.rooms.SendChat(c.Param("room"), service.SendChatInput{Message: req.Message, UserID: userID})
	if err != nil {
		if errors.Is(err, service.ErrRoomNotFound) {
			response.Error(c, http.StatusNotFound, "ROOM_NOT_FOUND", "Room tidak ditemukan")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal mengirim chat")
		return
	}
	response.Created(c, chat, "Chat berhasil dikirim")
}

func (h *RoomHandler) Chats(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	var before *time.Time
	if raw := c.Query("before"); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err == nil {
			before = &parsed
		}
	}
	chats, total, page, perPage, err := h.rooms.Chats(c.Param("room"), repository.ChatListFilter{Page: page, PerPage: perPage, Before: before})
	if err != nil {
		if errors.Is(err, service.ErrRoomNotFound) {
			response.Error(c, http.StatusNotFound, "ROOM_NOT_FOUND", "Room tidak ditemukan")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal mengambil chat room")
		return
	}
	response.WithMeta(c, http.StatusOK, chats, gin.H{"page": page, "per_page": perPage, "total": total}, "OK")
}

func (h *RoomHandler) handleRoomError(c *gin.Context, err error, fallback string) {
	if errors.Is(err, service.ErrRoomNotFound) {
		response.Error(c, http.StatusNotFound, "ROOM_NOT_FOUND", "Room tidak ditemukan")
		return
	}
	if errors.Is(err, service.ErrRoomForbidden) {
		response.Error(c, http.StatusForbidden, "FORBIDDEN", "Tidak punya akses ke room")
		return
	}
	response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", fallback)
}
