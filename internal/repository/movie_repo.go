package repository

import (
	"strings"
	"time"

	"backend-nobarkan/internal/domain"
	"gorm.io/gorm"
)

type MovieRepository struct {
	db *gorm.DB
}

type MovieListFilter struct {
	Page         int
	PerPage      int
	Search       string
	UploadedByID string
}

func NewMovieRepository(db *gorm.DB) *MovieRepository {
	return &MovieRepository{db: db}
}

func (r *MovieRepository) Create(movie *domain.Movie) error {
	return r.db.Create(movie).Error
}

func (r *MovieRepository) List(filter MovieListFilter) ([]domain.Movie, int64, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PerPage <= 0 || filter.PerPage > 100 {
		filter.PerPage = 20
	}

	query := r.db.Model(&domain.Movie{}).Where("movies.deleted_at IS NULL")
	if strings.TrimSpace(filter.Search) != "" {
		query = query.Where("title LIKE ?", "%"+strings.TrimSpace(filter.Search)+"%")
	}
	if filter.UploadedByID != "" {
		query = query.Where("uploaded_by = ?", filter.UploadedByID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var movies []domain.Movie
	err := query.Preload("Uploader").Order("created_at DESC").Limit(filter.PerPage).Offset((filter.Page - 1) * filter.PerPage).Find(&movies).Error
	return movies, total, err
}

func (r *MovieRepository) FindByID(id string) (*domain.Movie, error) {
	var movie domain.Movie
	if err := r.db.Preload("Uploader").Where("id = ? AND deleted_at IS NULL", id).First(&movie).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &movie, nil
}

func (r *MovieRepository) Update(movie *domain.Movie) error {
	return r.db.Save(movie).Error
}

func (r *MovieRepository) SoftDelete(id string) error {
	now := time.Now()
	return r.db.Model(&domain.Movie{}).Where("id = ?", id).Update("deleted_at", now).Error
}
