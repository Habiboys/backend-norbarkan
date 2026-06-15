package domain

import "time"

type RoomMode string

type RoomStatus string

type RoomMemberRole string

type RoomEventType string

const (
	RoomModeGDrive RoomMode = "gdrive"

	RoomStatusWaiting RoomStatus = "waiting"
	RoomStatusPlaying RoomStatus = "playing"
	RoomStatusPaused  RoomStatus = "paused"
	RoomStatusEnded   RoomStatus = "ended"

	RoomRoleHost   RoomMemberRole = "host"
	RoomRoleMember RoomMemberRole = "member"

	RoomEventPlay  RoomEventType = "play"
	RoomEventPause RoomEventType = "pause"
	RoomEventSeek  RoomEventType = "seek"
	RoomEventJoin  RoomEventType = "join"
	RoomEventLeave RoomEventType = "leave"
	RoomEventEnd   RoomEventType = "end"
)

type Room struct {
	ID          string     `json:"id" gorm:"type:char(36);primaryKey"`
	Name        string     `json:"name" gorm:"type:varchar(100);not null"`
	Code        string     `json:"code" gorm:"type:varchar(10);not null;uniqueIndex"`
	HostID      string     `json:"host_id" gorm:"type:char(36);not null;index"`
	MovieID     *string    `json:"movie_id" gorm:"type:char(36)"`
	Mode        RoomMode   `json:"mode" gorm:"type:enum('gdrive');not null;default:'gdrive'"`
	Status      RoomStatus `json:"status" gorm:"type:enum('waiting','playing','paused','ended');not null;default:'waiting';index"`
	CurrentTime float64    `json:"current_time" gorm:"not null;default:0"`
	IsPlaying   bool       `json:"is_playing" gorm:"not null;default:false"`
	IsPrivate   bool       `json:"is_private" gorm:"not null;default:false"`
	Password    *string    `json:"-" gorm:"type:varchar(255)"`
	MaxMembers  uint       `json:"max_members" gorm:"not null;default:10"`
	StartedAt   *time.Time `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Host        *User      `json:"host,omitempty" gorm:"foreignKey:HostID"`
	Movie       *Movie     `json:"movie,omitempty" gorm:"foreignKey:MovieID"`
}

type RoomMember struct {
	ID       string         `json:"id" gorm:"type:char(36);primaryKey"`
	RoomID   string         `json:"room_id" gorm:"type:char(36);not null;uniqueIndex:uq_room_user;index"`
	UserID   string         `json:"user_id" gorm:"type:char(36);not null;uniqueIndex:uq_room_user;index"`
	Role     RoomMemberRole `json:"role" gorm:"type:enum('host','member');not null;default:'member'"`
	IsReady  bool           `json:"is_ready" gorm:"not null;default:false"`
	IsMuted  bool           `json:"is_muted" gorm:"not null;default:false"`
	JoinedAt time.Time      `json:"joined_at"`
	LeftAt   *time.Time     `json:"left_at"`
	Room     *Room          `json:"room,omitempty" gorm:"foreignKey:RoomID"`
	User     *User          `json:"user,omitempty" gorm:"foreignKey:UserID"`
}

type RoomEvent struct {
	ID        string        `json:"id" gorm:"type:char(36);primaryKey"`
	RoomID    string        `json:"room_id" gorm:"type:char(36);not null;index"`
	UserID    string        `json:"user_id" gorm:"type:char(36);not null"`
	EventType RoomEventType `json:"event_type" gorm:"type:enum('play','pause','seek','join','leave','end');not null"`
	Payload   *string       `json:"payload" gorm:"type:json"`
	CreatedAt time.Time     `json:"created_at"`
	Room      *Room         `json:"room,omitempty" gorm:"foreignKey:RoomID"`
	User      *User         `json:"user,omitempty" gorm:"foreignKey:UserID"`
}
