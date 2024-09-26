package db

import (
	"go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
)

type UserStorageQuota struct {
	gorm.Model
	UserID      uint `gorm:"uniqueIndex"`
	User        models.User
	BytesStored uint64
}
