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

func (p *MovieSourceProvider) GetOriginalPath(movieID string) (string, error) {
	movie, err := p.repo.FindByID(movieID)
	if err != nil {
		return "", err
	}
	if movie == nil {
		return "", nil
	}

	// Already have stream URL
	if movie.OriginalPath != nil && *movie.OriginalPath != "" {
		return *movie.OriginalPath, nil
	}

	// No stream URL yet — fetch from sidecar
	if movie.ExternalURL == nil || *movie.ExternalURL == "" || p.sidecar == nil {
		return "", nil
	}

	streamInfo, err := p.sidecar.StreamURL(*movie.ExternalURL)
	if err != nil {
		log.Printf("[source] sidecar stream url failed for %s: %v", movieID, err)
		return "", nil
	}

	// Persist for next time
	movie.OriginalPath = &streamInfo.URL
	if err := p.repo.Update(movie); err != nil {
		log.Printf("[source] failed to update movie %s original_path: %v", movieID, err)
	}
	return streamInfo.URL, nil
}
