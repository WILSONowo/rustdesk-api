package model

import "time"

type ClientDownload struct {
	Platform string `json:"platform"`
	Arch     string `json:"arch"`
	Version  string `json:"version"`
	URL      string `json:"url"`
	Enabled  bool   `json:"enabled"`
}

// ClientResources is a singleton, persisted alongside account data.
type ClientResources struct {
	ID         uint             `gorm:"primaryKey" json:"-"`
	Downloads  []ClientDownload `gorm:"serializer:json;type:text" json:"downloads"`
	ImportCode string           `gorm:"type:text" json:"import_code"`
	UpdatedAt  time.Time        `json:"updated_at"`
}
