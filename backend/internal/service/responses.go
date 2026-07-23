package service

import "github.com/masterfabric/review-guard/mf-backend/internal/domain"

// SessionsResponse is GET /auth/sessions.
type SessionsResponse struct {
	Sessions []domain.SessionView `json:"sessions"`
}

// GamesResponse is GET /games.
type GamesResponse struct {
	Games []string `json:"games"`
}

// LogoutResponse is POST /auth/logout.
type LogoutResponse struct {
	LoggedOut bool `json:"logged_out"`
}

// PasswordChangedResponse is POST /auth/change-password.
type PasswordChangedResponse struct {
	PasswordChanged bool `json:"password_changed"`
}

// DeletedResponse is DELETE /reviews/{id}.
type DeletedResponse struct {
	Deleted bool `json:"deleted"`
}
