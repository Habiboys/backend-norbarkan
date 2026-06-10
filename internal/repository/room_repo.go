package repository

import (
	"time"

	"backend-nobarkan/internal/domain"
	"gorm.io/gorm"
)

type RoomRepository struct {
	db *gorm.DB
}

type RoomListFilter struct {
	Page    int
	PerPage int
	Status  string
}

func NewRoomRepository(db *gorm.DB) *RoomRepository {
	return &RoomRepository{db: db}
}

func (r *RoomRepository) Create(room *domain.Room) error {
	return r.db.Create(room).Error
}

func (r *RoomRepository) Update(room *domain.Room) error {
	return r.db.Save(room).Error
}

func (r *RoomRepository) ListPublic(filter RoomListFilter) ([]domain.Room, int64, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.PerPage <= 0 || filter.PerPage > 100 {
		filter.PerPage = 20
	}
	query := r.db.Model(&domain.Room{}).Where("is_private = ?", false)
	if filter.Status != "" {
		query = query.Where("status = ?", filter.Status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rooms []domain.Room
	err := query.Preload("Host").Preload("Movie").Preload("Movie.Uploader").Order("created_at DESC").Limit(filter.PerPage).Offset((filter.Page - 1) * filter.PerPage).Find(&rooms).Error
	return rooms, total, err
}

func (r *RoomRepository) FindByID(id string) (*domain.Room, error) {
	var room domain.Room
	if err := r.db.Preload("Host").Preload("Movie").Preload("Movie.Uploader").Where("id = ?", id).First(&room).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &room, nil
}

func (r *RoomRepository) FindByCode(code string) (*domain.Room, error) {
	var room domain.Room
	if err := r.db.Preload("Host").Preload("Movie").Preload("Movie.Uploader").Where("code = ?", code).First(&room).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &room, nil
}

func (r *RoomRepository) FindByHostID(hostID string) ([]domain.Room, error) {
	var rooms []domain.Room
	err := r.db.Preload("Host").Preload("Movie").Preload("Movie.Uploader").Where("host_id = ?", hostID).Order("created_at DESC").Find(&rooms).Error
	return rooms, err
}

func (r *RoomRepository) Close(id string) error {
	now := time.Now()
	return r.db.Model(&domain.Room{}).Where("id = ?", id).Updates(map[string]interface{}{"status": domain.RoomStatusEnded, "is_playing": false, "ended_at": now}).Error
}

func (r *RoomRepository) Delete(id string) error {
	return r.db.Delete(&domain.Room{}, "id = ?", id).Error
}

type RoomMemberRepository struct {
	db *gorm.DB
}

func NewRoomMemberRepository(db *gorm.DB) *RoomMemberRepository {
	return &RoomMemberRepository{db: db}
}

func (r *RoomMemberRepository) Create(member *domain.RoomMember) error {
	return r.db.Create(member).Error
}

func (r *RoomMemberRepository) UpsertActive(member *domain.RoomMember) error {
	existing, err := r.Find(member.RoomID, member.UserID)
	if err != nil {
		return err
	}
	if existing == nil {
		return r.Create(member)
	}
	existing.LeftAt = nil
	existing.Role = member.Role
	existing.JoinedAt = member.JoinedAt
	return r.db.Save(existing).Error
}

func (r *RoomMemberRepository) Find(roomID string, userID string) (*domain.RoomMember, error) {
	var member domain.RoomMember
	if err := r.db.Preload("User").Where("room_id = ? AND user_id = ?", roomID, userID).First(&member).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &member, nil
}

func (r *RoomMemberRepository) Leave(roomID string, userID string) error {
	now := time.Now()
	return r.db.Model(&domain.RoomMember{}).Where("room_id = ? AND user_id = ?", roomID, userID).Update("left_at", now).Error
}

func (r *RoomMemberRepository) ListActive(roomID string) ([]domain.RoomMember, error) {
	var members []domain.RoomMember
	err := r.db.Preload("User").Where("room_id = ? AND left_at IS NULL", roomID).Find(&members).Error
	return members, err
}

func (r *RoomMemberRepository) CountActive(roomID string) (int64, error) {
	var total int64
	err := r.db.Model(&domain.RoomMember{}).Where("room_id = ? AND left_at IS NULL", roomID).Count(&total).Error
	return total, err
}
