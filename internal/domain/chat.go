package domain

import "time"

type ChatType string

const (
	ChatTypeText     ChatType = "text"
	ChatTypeSystem   ChatType = "system"
	ChatTypeReaction ChatType = "reaction"
)

type Chat struct {
	ID        string    `json:"id" gorm:"type:char(36);primaryKey"`
	RoomID    string    `json:"room_id" gorm:"type:char(36);not null;index:idx_room_id_created,priority:1"`
	UserID    string    `json:"user_id" gorm:"type:char(36);not null"`
	Message   string    `json:"message" gorm:"type:text;not null"`
	Type      ChatType  `json:"type" gorm:"type:enum('text','system','reaction');not null;default:'text'"`
	CreatedAt time.Time `json:"created_at" gorm:"index:idx_room_id_created,priority:2"`
	Room      *Room     `json:"room,omitempty" gorm:"foreignKey:RoomID"`
	User      *User     `json:"user,omitempty" gorm:"foreignKey:UserID"`
}
