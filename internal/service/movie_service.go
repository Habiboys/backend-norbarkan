package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"backend-nobarkan/internal/config"
	"backend-nobarkan/internal/domain"
	"backend-nobarkan/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrMovieNotFound        = errors.New("movie not found")
	ErrMovieForbidden       = errors.New("movie forbidden")
	ErrInvalidExternalURL   = errors.New("invalid external url")
	ErrSidecarUnavailable   = errors.New("sidecar service unavailable")
)

type MovieService struct {
	movies       *repository.MovieRepository
	storage      config.StorageConfig
	sidecar      *SidecarClient

	extractorsCache     *ExtractorsResult
	extractorsCacheTime time.Time
	extractorsMu        sync.Mutex
}

const extractorsCacheTTL = 1 * time.Hour

type MovieResponse struct {
	ID              string                 `json:"id"`
	Title           string                 `json:"title"`
	Description     *string                `json:"description,omitempty"`
	SourceType      domain.MovieSourceType `json:"source_type"`
	ProviderName    *string                `json:"provider_name,omitempty"`
	ExternalURL     *string                `json:"external_url,omitempty"`
	ThumbnailURL    *string                `json:"thumbnail_url,omitempty"`
	Duration        *uint                  `json:"duration,omitempty"`
	FileSize        *uint64                `json:"file_size,omitempty"`
	TranscodeStatus domain.TranscodeStatus `json:"transcode_status"`
	UploadedBy      *UserResponse          `json:"uploaded_by,omitempty"`
	CreatedAt       time.Time              `json:"created_at"`
}

type MovieListResult struct {
	Data    []MovieResponse `json:"data"`
	Page    int             `json:"page"`
	PerPage int             `json:"per_page"`
	Total   int64           `json:"total"`
}

type CreateExternalMovieInput struct {
	URL          string
	Title        *string // optional, auto-filled from sidecar if empty
	Description  *string
	ThumbnailURL *string // optional, auto-filled from sidecar if empty
	UploadedBy   string
}

type UpdateMovieInput struct {
	Title        *string
	Description  *string
	ThumbnailURL *string
}

func NewMovieService(movies *repository.MovieRepository, storage config.StorageConfig, sidecar *SidecarClient) *MovieService {
	return &MovieService{movies: movies, storage: storage, sidecar: sidecar}
}

func (s *MovieService) List(filter repository.MovieListFilter) (*MovieListResult, error) {
	movies, total, err := s.movies.List(filter)
	if err != nil {
		return nil, err
	}
	items := make([]MovieResponse, 0, len(movies))
	for i := range movies {
		items = append(items, s.toResponse(&movies[i]))
	}
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PerPage <= 0 {
		filter.PerPage = 20
	}
	return &MovieListResult{Data: items, Page: filter.Page, PerPage: filter.PerPage, Total: total}, nil
}

func (s *MovieService) CreateExternal(input CreateExternalMovieInput) (*MovieResponse, error) {
	url := strings.TrimSpace(input.URL)
	if url == "" {
		return nil, fmt.Errorf("invalid external movie payload: url required")
	}

	// Extract metadata from sidecar
	meta, err := s.sidecar.Extract(url)
	if err != nil {
		return nil, fmt.Errorf("sidecar extract: %w", err)
	}

	title := meta.Title
	if input.Title != nil && strings.TrimSpace(*input.Title) != "" {
		title = strings.TrimSpace(*input.Title)
	}

	thumbnail := normalizeStringPtr(input.ThumbnailURL)
	if thumbnail == nil && meta.Thumbnail != "" {
		t := meta.Thumbnail
		thumbnail = &t
	}

	providerName := meta.Extractor
	if providerName == "" {
		providerName = "external"
	}

	var duration *uint
	if meta.Duration > 0 {
		d := uint(meta.Duration)
		duration = &d
	}

	externalURL := url
	if meta.WebpageURL != "" {
		externalURL = meta.WebpageURL
	}

	movie := &domain.Movie{
		ID:              uuid.NewString(),
		Title:           title,
		Description:     normalizeStringPtr(input.Description),
		SourceType:      domain.MovieSourceExternal,
		ProviderName:    &providerName,
		ExternalURL:     &externalURL,
		ThumbnailURL:    thumbnail,
		Duration:        duration,
		TranscodeStatus: domain.TranscodeDone,
		UploadedBy:      input.UploadedBy,
	}

	// Get direct stream URL via sidecar
	streamInfo, err := s.sidecar.StreamURL(url)
	if err == nil {
		movie.OriginalPath = &streamInfo.URL
	}

	if err := s.movies.Create(movie); err != nil {
		return nil, err
	}

	response := s.toResponse(movie)
	return &response, nil
}

func (s *MovieService) Get(id string) (*MovieResponse, error) {
	movie, err := s.movies.FindByID(id)
	if err != nil {
		return nil, err
	}
	if movie == nil {
		return nil, ErrMovieNotFound
	}
	response := s.toResponse(movie)
	return &response, nil
}

func (s *MovieService) Delete(id string, userID string) error {
	movie, err := s.movies.FindByID(id)
	if err != nil {
		return err
	}
	if movie == nil {
		return ErrMovieNotFound
	}
	if movie.UploadedBy != userID {
		return ErrMovieForbidden
	}
	return s.movies.SoftDelete(id)
}

func (s *MovieService) Update(id string, userID string, input UpdateMovieInput) (*MovieResponse, error) {
	movie, err := s.movies.FindByID(id)
	if err != nil {
		return nil, err
	}
	if movie == nil {
		return nil, ErrMovieNotFound
	}
	if movie.UploadedBy != userID {
		return nil, ErrMovieForbidden
	}

	if input.Title != nil {
		movie.Title = *input.Title
	}
	movie.Description = updateStringPtr(movie.Description, input.Description)
	movie.ThumbnailURL = updateStringPtr(movie.ThumbnailURL, input.ThumbnailURL)

	if err := s.movies.Update(movie); err != nil {
		return nil, err
	}
	response := s.toResponse(movie)
	return &response, nil
}

func (s *MovieService) TranscodeStatus(id string) (map[string]interface{}, error) {
	movie, err := s.movies.FindByID(id)
	if err != nil {
		return nil, err
	}
	if movie == nil {
		return nil, ErrMovieNotFound
	}
	return map[string]interface{}{"id": movie.ID, "transcode_status": string(movie.TranscodeStatus), "progress": 100}, nil
}

func (s *MovieService) ListExtractors() (*ExtractorsResult, error) {
	s.extractorsMu.Lock()
	defer s.extractorsMu.Unlock()

	if s.extractorsCache != nil && time.Since(s.extractorsCacheTime) < extractorsCacheTTL {
		return s.extractorsCache, nil
	}

	result, err := s.sidecar.ListExtractors()
	if err != nil {
		return nil, err
	}

	s.extractorsCache = result
	s.extractorsCacheTime = time.Now()
	return result, nil
}

func (s *MovieService) ListFormats(videoURL string) (*FormatsResult, error) {
	return s.sidecar.ListFormats(videoURL)
}

func (s *MovieService) toResponse(movie *domain.Movie) MovieResponse {
	var uploader *UserResponse
	if movie.Uploader != nil {
		value := toUserResponse(movie.Uploader)
		uploader = &value
	}

	return MovieResponse{
		ID:              movie.ID,
		Title:           movie.Title,
		Description:     movie.Description,
		SourceType:      movie.SourceType,
		ProviderName:    movie.ProviderName,
		ExternalURL:     movie.ExternalURL,
		ThumbnailURL:    movie.ThumbnailURL,
		Duration:        movie.Duration,
		FileSize:        movie.FileSize,
		TranscodeStatus: movie.TranscodeStatus,
		UploadedBy:      uploader,
		CreatedAt:       movie.CreatedAt,
	}
}

func normalizeStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

func updateStringPtr(current *string, input *string) *string {
	if input == nil {
		return current
	}
	trimmed := strings.TrimSpace(*input)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}
