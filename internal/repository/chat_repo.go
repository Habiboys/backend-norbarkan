package repository

import (
	"time"

	"backend-nobarkan/internal/domain"
	"gorm.io/gorm"
)

type ChatRepository struct {
	db *gorm.DB
}

type ChatListFilter struct {
	Page    int
	PerPage int
	Before  *time.Time
}

func NewChatRepository(db *gorm.DB) *ChatRepository {
	return &ChatRepository{db: db}
}

func (r *ChatRepository) ListByRoom(roomID string, filter ChatListFilter) ([]domain.Chat, int64, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PerPage <= 0 || filter.PerPage > 100 {
		filter.PerPage = 50
	}
	query := r.db.Model(&domain.Chat{}).Where("room_id = ?", roomID)
	if filter.Before != nil {
		query = query.Where("created_at < ?", *filter.Before)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var chats []domain.Chat
	err := query.Preload("User").Order("created_at DESC").Limit(filter.PerPage).Offset((filter.Page - 1) * filter.PerPage).Find(&chats).Error
	return chats, total, err
}

func (r *ChatRepository) Create(chat *domain.Chat) error {
	return r.db.Create(chat).Error
}
