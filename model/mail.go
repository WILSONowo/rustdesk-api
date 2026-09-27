package model

import "time"

// EmailIdentity reserves a verified mailbox atomically, including across workers.
type EmailIdentity struct {
	Email  string `gorm:"primaryKey;size:254"`
	UserID uint   `gorm:"uniqueIndex;not null"`
}

type EmailChallenge struct {
	ID             string `gorm:"primaryKey;size:64"`
	Purpose        string `gorm:"size:16;index"`
	Email          string `gorm:"size:254;index"`
	UserID         uint   `gorm:"index"`
	CodeHash       string `gorm:"size:64"`
	CredentialHash string `gorm:"size:64"`
	Attempts       int
	CreatedAt      time.Time `gorm:"index"`
	ExpiresAt      time.Time `gorm:"index"`
	Consumed       bool
}

type MailMessage struct {
	ID          uint   `gorm:"primaryKey"`
	To          string `gorm:"size:254"`
	Subject     string `gorm:"size:255"`
	Body        string `gorm:"type:text" json:"-"`
	ChallengeID string `gorm:"size:64;index"`
	State       string `gorm:"size:16;index"` // queued, sending, sent, failed, expired
	Attempts    int
	AvailableAt time.Time `gorm:"index"`
	ExpiresAt   time.Time
	CreatedAt   time.Time `gorm:"index"`
	UpdatedAt   time.Time
	ErrorCode   string `gorm:"size:32"` // never persist SMTP responses or credentials
}

type MailRate struct {
	Key       string `gorm:"primaryKey;size:96"`
	Count     int
	ExpiresAt time.Time `gorm:"index"`
}
