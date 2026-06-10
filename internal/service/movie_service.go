package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"backend-nobarkan/internal/config"
	"backend-nobarkan/internal/domain"
	"backend-nobarkan/internal/repository"
	"github.com/google/uuid"
)

var (
	ErrMovieNotFound         = errors.New("movie not found")
	ErrMovieForbidden        = errors.New("movie forbidden")
	ErrInvalidGoogleDriveURL = errors.New("invalid google drive url")
)

type MovieService struct {
	movies  *repository.MovieRepository
	storage config.StorageConfig
}

type MovieResponse struct {
	ID              string                 `json:"id"`
	Title           string                 `json:"title"`
	Description     *string                `json:"description,omitempty"`
	SourceType      domain.MovieSourceType `json:"source_type"`
	ProviderName    *string                `json:"provider_name,omitempty"`
	ExternalURL     *string                `json:"external_url,omitempty"`
	DriveFileID     *string                `json:"drive_file_id,omitempty"`
	DriveURL        *string                `json:"drive_url,omitempty"`
	DrivePreviewURL *string                `json:"drive_preview_url,omitempty"`
	DriveDirectURL  *string                `json:"drive_direct_url,omitempty"`
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

type CreateGDriveMovieInput struct {
	Title        string
	Description  *string
	DriveURL     string
	ThumbnailURL *string
	UploadedBy   string
}

type UpdateMovieInput struct {
	Title        *string
	Description  *string
	DriveURL     *string
	ThumbnailURL *string
}

func NewMovieService(movies *repository.MovieRepository, storage config.StorageConfig) *MovieService {
	return &MovieService{movies: movies, storage: storage}
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

func (s *MovieService) CreateGDrive(input CreateGDriveMovieInput) (*MovieResponse, error) {
	title := strings.TrimSpace(input.Title)
	driveURL := strings.TrimSpace(input.DriveURL)
	if title == "" || driveURL == "" {
		return nil, fmt.Errorf("invalid google drive movie payload")
	}

	fileID, err := extractGoogleDriveFileID(driveURL)
	if err != nil {
		return nil, err
	}

	providerName := "Google Drive"
	movie := &domain.Movie{
		ID:              uuid.NewString(),
		Title:           title,
		Description:     normalizeStringPtr(input.Description),
		SourceType:      domain.MovieSourceGDrive,
		ProviderName:    &providerName,
		ExternalURL:     &driveURL,
		DriveFileID:     &fileID,
		DriveURL:        &driveURL,
		ThumbnailURL:    normalizeStringPtr(input.ThumbnailURL),
		TranscodeStatus: domain.TranscodeDone,
		UploadedBy:      input.UploadedBy,
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

	if input.DriveURL != nil {
		driveURL := strings.TrimSpace(*input.DriveURL)
		if driveURL != "" {
			fileID, err := extractGoogleDriveFileID(driveURL)
			if err != nil {
				return nil, ErrInvalidGoogleDriveURL
			}
			movie.DriveURL = &driveURL
			movie.DriveFileID = &fileID
			movie.ExternalURL = &driveURL
		}
	}

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
	return map[string]interface{}{"id": movie.ID, "transcode_status": "not_applicable", "progress": 100}, nil
}

func (s *MovieService) toResponse(movie *domain.Movie) MovieResponse {
	var uploader *UserResponse
	if movie.Uploader != nil {
		value := toUserResponse(movie.Uploader)
		uploader = &value
	}

	var drivePreviewURL *string
	if movie.DriveFileID != nil && *movie.DriveFileID != "" {
		value := "https://drive.google.com/file/d/" + *movie.DriveFileID + "/preview"
		drivePreviewURL = &value
	}

	driveURL := movie.DriveURL
	if driveURL == nil {
		driveURL = movie.ExternalURL
	}

	var driveDirectURL *string
	if movie.DriveFileID != nil && *movie.DriveFileID != "" {
		value := resolveDriveDirectURL(*movie.DriveFileID)
		driveDirectURL = &value
	}

	return MovieResponse{
		ID:              movie.ID,
		Title:           movie.Title,
		Description:     movie.Description,
		SourceType:      movie.SourceType,
		ProviderName:    movie.ProviderName,
		ExternalURL:     movie.ExternalURL,
		DriveFileID:     movie.DriveFileID,
		DriveURL:        driveURL,
		DrivePreviewURL: drivePreviewURL,
		DriveDirectURL:  driveDirectURL,
		ThumbnailURL:    movie.ThumbnailURL,
		Duration:        movie.Duration,
		FileSize:        movie.FileSize,
		TranscodeStatus: movie.TranscodeStatus,
		UploadedBy:      uploader,
		CreatedAt:       movie.CreatedAt,
	}
}

func resolveDriveDirectURL(fileID string) string {
	return "/proxy/drive/" + fileID
}

func extractGoogleDriveFileID(rawURL string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || parsed.Host == "" {
		return "", ErrInvalidGoogleDriveURL
	}

	host := strings.ToLower(parsed.Host)
	allowedHosts := map[string]bool{
		"drive.google.com":             true,
		"www.drive.google.com":         true,
		"drive.usercontent.google.com": true,
	}
	if !allowedHosts[host] {
		return "", ErrInvalidGoogleDriveURL
	}

	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i, part := range parts {
		if part == "d" && i+1 < len(parts) && strings.TrimSpace(parts[i+1]) != "" {
			return strings.TrimSpace(parts[i+1]), nil
		}
	}

	if id := strings.TrimSpace(parsed.Query().Get("id")); id != "" {
		return id, nil
	}

	return "", ErrInvalidGoogleDriveURL
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
