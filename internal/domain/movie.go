package domain

import "time"

type MovieSourceType string

type TranscodeStatus string

const (
	MovieSourceGDrive MovieSourceType = "gdrive"

	TranscodePending    TranscodeStatus = "pending"
	TranscodeProcessing TranscodeStatus = "processing"
	TranscodeDone       TranscodeStatus = "done"
	TranscodeFailed     TranscodeStatus = "failed"
)

type Movie struct {
	ID              string          `json:"id" gorm:"type:char(36);primaryKey"`
	Title           string          `json:"title" gorm:"type:varchar(255);not null"`
	Description     *string         `json:"description" gorm:"type:text"`
	SourceType      MovieSourceType `json:"source_type" gorm:"type:enum('gdrive');not null;default:'gdrive';index"`
	ProviderName    *string         `json:"provider_name" gorm:"type:varchar(100)"`
	ExternalURL     *string         `json:"external_url" gorm:"type:varchar(1000)"`
	DriveFileID     *string         `json:"drive_file_id" gorm:"type:varchar(255);index"`
	DriveURL        *string         `json:"drive_url" gorm:"type:varchar(1000)"`
	OriginalPath    *string         `json:"original_path" gorm:"type:varchar(500)"`
	HLSPath         *string         `json:"hls_path" gorm:"type:varchar(500)"`
	ThumbnailURL    *string         `json:"thumbnail_url" gorm:"type:varchar(500)"`
	Duration        *uint           `json:"duration"`
	FileSize        *uint64         `json:"file_size"`
	TranscodeStatus TranscodeStatus `json:"transcode_status" gorm:"type:enum('pending','processing','done','failed');default:'done'"`
	UploadedBy      string          `json:"uploaded_by" gorm:"type:char(36);not null;index"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
	DeletedAt       *time.Time      `json:"deleted_at,omitempty" gorm:"index"`
	Uploader        *User           `json:"uploader,omitempty" gorm:"foreignKey:UploadedBy"`
}
