package domain

import "time"

type User struct {
	ID        string     `json:"id" gorm:"type:char(36);primaryKey"`
	Name      string     `json:"name" gorm:"type:varchar(100);not null"`
	Email     string     `json:"email" gorm:"type:varchar(150);not null;uniqueIndex"`
	Password  string     `json:"-" gorm:"type:varchar(255);not null"`
	AvatarURL *string    `json:"avatar_url" gorm:"type:varchar(500)"`
	IsActive  bool       `json:"is_active" gorm:"not null;default:true"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty" gorm:"index"`
}

type RefreshToken struct {
	ID        string    `json:"id" gorm:"type:char(36);primaryKey"`
	UserID    string    `json:"user_id" gorm:"type:char(36);not null;index"`
	Token     string    `json:"-" gorm:"type:varchar(500);not null;uniqueIndex"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	User      *User     `json:"user,omitempty" gorm:"foreignKey:UserID"`
}
