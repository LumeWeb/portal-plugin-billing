package db

import (
	"go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
	"time"
)

type UserQuota struct {
	gorm.Model
	UserID          uint `gorm:"index"`
	User            models.User
	Date            time.Time `gorm:"index"`
	BytesStored     uint64
	BytesUploaded   uint64
	BytesDownloaded uint64
}
