package handler

import (
	"errors"
	"net/http"
	"strconv"

	"backend-nobarkan/internal/pkg/response"
	"backend-nobarkan/internal/repository"
	"backend-nobarkan/internal/service"
	"github.com/gin-gonic/gin"
)

type MovieHandler struct {
	movies *service.MovieService
}

type createGDriveMovieRequest struct {
	Title        string  `json:"title" binding:"required"`
	Description  *string `json:"description"`
	DriveURL     string  `json:"drive_url" binding:"required"`
	ThumbnailURL *string `json:"thumbnail_url"`
}

type updateMovieRequest struct {
	Title        *string `json:"title"`
	Description  *string `json:"description"`
	DriveURL     *string `json:"drive_url"`
	ThumbnailURL *string `json:"thumbnail_url"`
}

func NewMovieHandler(movies *service.MovieService) *MovieHandler {
	return &MovieHandler{movies: movies}
}

func (h *MovieHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	userID := currentUserID(c)
	result, err := h.movies.List(repository.MovieListFilter{
		Page:         page,
		PerPage:      perPage,
		Search:       c.Query("search"),
		UploadedByID: userID,
	})
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal mengambil data movie")
		return
	}

	response.WithMeta(c, http.StatusOK, result.Data, gin.H{"page": result.Page, "per_page": result.PerPage, "total": result.Total}, "OK")
}

func (h *MovieHandler) CreateGDrive(c *gin.Context) {
	userID := currentUserID(c)
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}

	var req createGDriveMovieRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "Payload movie Google Drive tidak valid")
		return
	}

	movie, err := h.movies.CreateGDrive(service.CreateGDriveMovieInput{
		Title:        req.Title,
		Description:  req.Description,
		DriveURL:     req.DriveURL,
		ThumbnailURL: req.ThumbnailURL,
		UploadedBy:   userID,
	})
	if err != nil {
		if errors.Is(err, service.ErrInvalidGoogleDriveURL) {
			response.Error(c, http.StatusBadRequest, "INVALID_GDRIVE_URL", "Link Google Drive tidak valid")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal membuat movie Google Drive")
		return
	}

	response.Created(c, movie, "Movie Google Drive berhasil dibuat")
}

func (h *MovieHandler) Get(c *gin.Context) {
	movie, err := h.movies.Get(c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrMovieNotFound) {
			response.Error(c, http.StatusNotFound, "MOVIE_NOT_FOUND", "Film tidak ditemukan")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal mengambil detail movie")
		return
	}
	response.OK(c, movie, "OK")
}

func (h *MovieHandler) Delete(c *gin.Context) {
	userID := currentUserID(c)
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}

	if err := h.movies.Delete(c.Param("id"), userID); err != nil {
		if errors.Is(err, service.ErrMovieNotFound) {
			response.Error(c, http.StatusNotFound, "MOVIE_NOT_FOUND", "Film tidak ditemukan")
			return
		}
		if errors.Is(err, service.ErrMovieForbidden) {
			response.Error(c, http.StatusForbidden, "FORBIDDEN", "Tidak punya akses menghapus film")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal menghapus movie")
		return
	}

	response.OK(c, nil, "Film berhasil dihapus")
}

func (h *MovieHandler) Update(c *gin.Context) {
	userID := currentUserID(c)
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}

	var req updateMovieRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "Payload update movie tidak valid")
		return
	}

	movie, err := h.movies.Update(c.Param("id"), userID, service.UpdateMovieInput{
		Title:        req.Title,
		Description:  req.Description,
		DriveURL:     req.DriveURL,
		ThumbnailURL: req.ThumbnailURL,
	})
	if err != nil {
		if errors.Is(err, service.ErrMovieNotFound) {
			response.Error(c, http.StatusNotFound, "MOVIE_NOT_FOUND", "Film tidak ditemukan")
			return
		}
		if errors.Is(err, service.ErrMovieForbidden) {
			response.Error(c, http.StatusForbidden, "FORBIDDEN", "Tidak punya akses mengupdate film")
			return
		}
		if errors.Is(err, service.ErrInvalidGoogleDriveURL) {
			response.Error(c, http.StatusBadRequest, "INVALID_GDRIVE_URL", "Link Google Drive tidak valid")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal mengupdate movie")
		return
	}

	response.OK(c, movie, "Film berhasil diupdate")
}

func (h *MovieHandler) TranscodeStatus(c *gin.Context) {
	status, err := h.movies.TranscodeStatus(c.Param("id"))
	if err != nil {
		if errors.Is(err, service.ErrMovieNotFound) {
			response.Error(c, http.StatusNotFound, "MOVIE_NOT_FOUND", "Film tidak ditemukan")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal mengambil status transcode")
		return
	}

	response.OK(c, status, "OK")
}
