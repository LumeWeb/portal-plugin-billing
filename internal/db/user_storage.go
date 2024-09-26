package db

import (
	"go.lumeweb.com/portal/db/models"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"
)

var _ schema.Tabler = (*UserUpload)(nil)

type UserStorage struct {
	gorm.Model
	UserID   uint `gorm:"index"`
	User     models.User
	UploadID uint `gorm:"index"`
	Upload   models.Upload
	Bytes    uint64
	IsAdd    bool   `gorm:"index"`
	IP       string `gorm:"index"`
}

func (u *UserStorage) TableName() string {
	return "user_storage"
}
