package model

import "time"

const (
	GMGNAPIKeyStatusAvailable   = "available"
	GMGNAPIKeyStatusUnavailable = "unavailable"
)

type GMGNAPIKey struct {
	ID                   string     `json:"id"`
	KeyMask              string     `json:"keyMask"`
	Status               string     `json:"status"`
	UnavailableReason    string     `json:"unavailableReason"`
	UnavailableAt        *time.Time `json:"unavailableAt,omitempty"`
	LastSuccessfulUsedAt *time.Time `json:"lastSuccessfulUsedAt,omitempty"`
	CreatedAt            time.Time  `json:"createdAt"`
	UpdatedAt            time.Time  `json:"updatedAt"`
}
