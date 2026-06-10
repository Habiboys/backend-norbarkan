package handler

import (
	"errors"
	"net/http"

	"backend-nobarkan/internal/pkg/response"
	"backend-nobarkan/internal/service"
	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	auth *service.AuthService
}

type registerRequest struct {
	Name     string `json:"name" binding:"required"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type loginRequest struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func NewAuthHandler(auth *service.AuthService) *AuthHandler {
	return &AuthHandler{auth: auth}
}

func (h *AuthHandler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "Payload register tidak valid")
		return
	}

	result, err := h.auth.Register(req.Name, req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrEmailAlreadyRegistered) {
			response.Error(c, http.StatusConflict, "EMAIL_ALREADY_REGISTERED", "Email sudah terdaftar")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal register user")
		return
	}

	response.Created(c, result, "Register berhasil")
}

func (h *AuthHandler) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "Payload login tidak valid")
		return
	}

	result, err := h.auth.Login(req.Email, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			response.Error(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Email atau password salah")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal login")
		return
	}

	response.OK(c, result, "Login berhasil")
}

func (h *AuthHandler) Refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "Payload refresh tidak valid")
		return
	}

	result, err := h.auth.Refresh(req.RefreshToken)
	if err != nil {
		if errors.Is(err, service.ErrRefreshTokenInvalid) {
			response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Refresh token tidak valid atau expired")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal refresh token")
		return
	}

	response.OK(c, result, "Token berhasil diperbarui")
}

func (h *AuthHandler) Logout(c *gin.Context) {
	var req logoutRequest
	_ = c.ShouldBindJSON(&req)

	if err := h.auth.Logout(req.RefreshToken); err != nil {
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal logout")
		return
	}

	response.OK(c, nil, "Logged out")
}

func (h *AuthHandler) Me(c *gin.Context) {
	userID, ok := c.Get("user_id")
	if !ok {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}

	user, err := h.auth.Me(userID.(string))
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			response.Error(c, http.StatusNotFound, "USER_NOT_FOUND", "User tidak ditemukan")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal mengambil data user")
		return
	}

	response.OK(c, user, "OK")
}
