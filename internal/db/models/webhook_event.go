package models

import (
	"time"

	"gorm.io/gorm"
)

// WebhookEvent represents a processed webhook event for deduplication purposes
type WebhookEvent struct {
	gorm.Model
	GatewayType string
	EventID     string
	EventType   string
	ProcessedAt time.Time
	Payload     []byte
}

// TableName sets the table name for WebhookEvent
func (WebhookEvent) TableName() string {
	return "billing_webhook_events"
}
