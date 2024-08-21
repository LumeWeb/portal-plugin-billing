package db

import (
	"go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
)

type Download struct {
	gorm.Model
	UserID   uint `gorm:"index"`
	User     models.User
	UploadID uint `gorm:"index"`
	Upload   models.Upload
	Bytes    uint64
	IP       string `gorm:"index"`
}
