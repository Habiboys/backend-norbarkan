package handler

import (
	"errors"
	"net/http"

	"backend-nobarkan/internal/pkg/response"
	"backend-nobarkan/internal/service"
	"github.com/gin-gonic/gin"
)

type UserHandler struct {
	users *service.UserService
}

type updateProfileRequest struct {
	Name      string  `json:"name"`
	AvatarURL *string `json:"avatar_url"`
}

type changePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

func NewUserHandler(users *service.UserService) *UserHandler {
	return &UserHandler{users: users}
}

func (h *UserHandler) UpdateMe(c *gin.Context) {
	userID := currentUserID(c)
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}

	var req updateProfileRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "Payload update profile tidak valid")
		return
	}

	user, err := h.users.UpdateProfile(userID, req.Name, req.AvatarURL)
	if err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			response.Error(c, http.StatusNotFound, "USER_NOT_FOUND", "User tidak ditemukan")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal update profile")
		return
	}

	response.OK(c, user, "Profile berhasil diupdate")
}

func (h *UserHandler) ChangePassword(c *gin.Context) {
	userID := currentUserID(c)
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}

	var req changePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "Payload ubah password tidak valid")
		return
	}

	if err := h.users.ChangePassword(userID, req.OldPassword, req.NewPassword); err != nil {
		if errors.Is(err, service.ErrUserNotFound) {
			response.Error(c, http.StatusNotFound, "USER_NOT_FOUND", "User tidak ditemukan")
			return
		}
		if errors.Is(err, service.ErrOldPasswordWrong) || errors.Is(err, service.ErrInvalidCredentials) {
			response.Error(c, http.StatusUnauthorized, "INVALID_CREDENTIALS", "Password lama salah atau password baru tidak valid")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal ubah password")
		return
	}

	response.OK(c, nil, "Password berhasil diubah")
}

func currentUserID(c *gin.Context) string {
	userID, ok := c.Get("user_id")
	if !ok {
		return ""
	}
	value, ok := userID.(string)
	if !ok {
		return ""
	}
	return value
}
