package handler

import (
	"backend-nobarkan/internal/repository"
	"backend-nobarkan/internal/service"
	"log"
)

// MovieSourceProvider implements MediaSourceProvider by looking up movies via repository.
// Auto-populates OriginalPath from sidecar when missing.
type MovieSourceProvider struct {
	repo    *repository.MovieRepository
	sidecar *service.SidecarClient
}

func NewMovieSourceProvider(repo *repository.MovieRepository, sidecar *service.SidecarClient) *MovieSourceProvider {
	return &MovieSourceProvider{repo: repo, sidecar: sidecar}
}

func (p *MovieSourceProvider) GetOriginalPath(movieID string, formatID string) (string, error) {
	movie, err := p.repo.FindByID(movieID)
	if err != nil {
		return "", err
	}
	if movie == nil {
		return "", nil
	}

	// Always fetch fresh stream URL — YouTube URLs expire
	if movie.ExternalURL == nil || *movie.ExternalURL == "" || p.sidecar == nil {
		// Fallback to cached if sidecar unavailable
		if movie.OriginalPath != nil && *movie.OriginalPath != "" {
			return *movie.OriginalPath, nil
		}
		return "", nil
	}

	streamInfo, err := p.sidecar.StreamURLWithFormat(*movie.ExternalURL, formatID)
	if err != nil {
		log.Printf("[source] sidecar stream url failed for %s: %v", movieID, err)
		// Fallback to cached URL
		if movie.OriginalPath != nil && *movie.OriginalPath != "" {
			return *movie.OriginalPath, nil
		}
		return "", nil
	}

	// Update cache for next call (in case sidecar temporarily down)
	if streamInfo.URL != *movie.OriginalPath {
		movie.OriginalPath = &streamInfo.URL
		_ = p.repo.Update(movie)
	}
	return streamInfo.URL, nil
}
