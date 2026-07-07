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

type createExternalMovieRequest struct {
	URL          string  `json:"url" binding:"required"`
	Title        *string `json:"title"`
	Description  *string `json:"description"`
	ThumbnailURL *string `json:"thumbnail_url"`
}

type updateMovieRequest struct {
	Title        *string `json:"title"`
	Description  *string `json:"description"`
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

func (h *MovieHandler) CreateFromURL(c *gin.Context) {
	userID := currentUserID(c)
	if userID == "" {
		response.Error(c, http.StatusUnauthorized, "TOKEN_INVALID", "Token tidak valid")
		return
	}

	var req createExternalMovieRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, "VALIDATION_ERROR", "URL film wajib diisi")
		return
	}

	movie, err := h.movies.CreateExternal(service.CreateExternalMovieInput{
		URL:          req.URL,
		Title:        req.Title,
		Description:  req.Description,
		ThumbnailURL: req.ThumbnailURL,
		UploadedBy:   userID,
	})
	if err != nil {
		if errors.Is(err, service.ErrInvalidExternalURL) {
			response.Error(c, http.StatusBadRequest, "INVALID_URL", "Link tidak valid")
			return
		}
		if errors.Is(err, service.ErrSidecarUnavailable) {
			response.Error(c, http.StatusServiceUnavailable, "SIDECAR_UNAVAILABLE", "Service extractor sedang tidak tersedia. Coba lagi nanti.")
			return
		}
		response.Error(c, http.StatusInternalServerError, "INTERNAL_SERVER_ERROR", "Gagal membuat movie: "+err.Error())
		return
	}

	response.Created(c, movie, "Movie berhasil dibuat")
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

func (h *MovieHandler) ListExtractors(c *gin.Context) {
	result, err := h.movies.ListExtractors()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, "EXTRACTORS_ERROR", "Gagal mengambil daftar extractor: "+err.Error())
		return
	}
	response.OK(c, result, "OK")
}
