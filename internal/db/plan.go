package db

import "gorm.io/gorm"

type Plan struct {
	gorm.Model
	Identifier string
	Storage    uint64
	Upload     uint64
	Download   uint64
}
