package db

import (
	"go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var _ schema.Tabler = (*UserUpload)(nil)

type UserDownload struct {
	gorm.Model
	UserID   uint `gorm:"index"`
	User     models.User
	UploadID uint `gorm:"index"`
	Upload   models.Upload
	Bytes    uint64
	IP       string `gorm:"index"`
}

func (u *UserDownload) TableName() string {
	return "user_download"
}
