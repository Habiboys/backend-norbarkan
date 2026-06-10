package repository

import (
	"backend-nobarkan/internal/domain"
	"gorm.io/gorm"
)

type RefreshTokenRepository struct {
	db *gorm.DB
}

func NewRefreshTokenRepository(db *gorm.DB) *RefreshTokenRepository {
	return &RefreshTokenRepository{db: db}
}

func (r *RefreshTokenRepository) Create(token *domain.RefreshToken) error {
	return r.db.Create(token).Error
}

func (r *RefreshTokenRepository) FindByTokenHash(tokenHash string) (*domain.RefreshToken, error) {
	var token domain.RefreshToken
	if err := r.db.Where("token = ?", tokenHash).First(&token).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &token, nil
}

func (r *RefreshTokenRepository) DeleteByTokenHash(tokenHash string) error {
	return r.db.Where("token = ?", tokenHash).Delete(&domain.RefreshToken{}).Error
}

func (r *RefreshTokenRepository) DeleteByUserID(userID string) error {
	return r.db.Where("user_id = ?", userID).Delete(&domain.RefreshToken{}).Error
}
