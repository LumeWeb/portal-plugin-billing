package db

import (
	"go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
	"time"
)

type UserBandwidthQuota struct {
	gorm.Model
	UserID          uint `gorm:"uniqueIndex"`
	User            models.User
	Date            time.Time `gorm:"index"`
	BytesUploaded   uint64
	BytesDownloaded uint64
}
