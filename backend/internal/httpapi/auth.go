package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/service"
)

const (
	maxAuthBody   = 8 << 10  // 8 KiB for auth JSON
	maxReviewBody = 64 << 10 // 64 KiB for review payloads
)

// AuthHandler serves the 8 auth endpoints.
type AuthHandler struct {
	svc *service.AuthService
}

// NewAuthHandler constructs an AuthHandler.
func NewAuthHandler(svc *service.AuthService) *AuthHandler {
	return &AuthHandler{svc: svc}
}

type registerRequest struct {
	Email    string `json:"email"`
	Name     string `json:"name"`
	Password string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type updateMeRequest struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	RefreshToken    string `json:"refresh_token"` // optional: keep this session
}

// Register handles POST /auth/register.
func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	tokens, err := h.svc.Register(r.Context(), req.Email, req.Name, req.Password)
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusCreated, tokens)
}

// Login handles POST /auth/login.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	tokens, err := h.svc.Login(r.Context(), req.Email, req.Password)
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, tokens)
}

// Refresh handles POST /auth/refresh.
func (h *AuthHandler) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	tokens, err := h.svc.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, tokens)
}

// Logout handles POST /auth/logout.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	var req logoutRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if err := h.svc.Logout(r.Context(), req.RefreshToken); err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, service.LogoutResponse{LoggedOut: true})
}

// Me handles GET /auth/me.
func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	user, err := h.svc.Me(r.Context(), userID)
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	user.PasswordHash = ""
	WriteOK(w, http.StatusOK, user)
}

// UpdateMe handles PATCH /auth/me.
func (h *AuthHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req updateMeRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	user, err := h.svc.UpdateMe(r.Context(), userID, req.Email, req.Name)
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	user.PasswordHash = ""
	WriteOK(w, http.StatusOK, user)
}

// ChangePassword handles POST /auth/change-password.
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req changePasswordRequest
	if err := decodeJSON(r, &req); err != nil {
		WriteErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	if err := h.svc.ChangePassword(r.Context(), userID, req.CurrentPassword, req.NewPassword, req.RefreshToken); err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, service.PasswordChangedResponse{PasswordChanged: true})
}

// Sessions handles GET /auth/sessions.
// Current session is marked via X-Refresh-Token header only (never query string).
func (h *AuthHandler) Sessions(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	current := r.Header.Get("X-Refresh-Token")
	sessions, err := h.svc.ListSessions(r.Context(), userID, current)
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, service.SessionsResponse{Sessions: sessions})
}

// Games handles GET /games (auth-scoped).
func (h *AuthHandler) Games(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	games, err := h.svc.ListGames(r.Context(), userID)
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, service.GamesResponse{Games: games})
}

func decodeJSON(r *http.Request, dst any) error {
	return decodeJSONLimited(r, dst, maxAuthBody)
}

func decodeJSONLimited(r *http.Request, dst any, maxBytes int64) error {
	defer r.Body.Close()
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBytes))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

func mapAuthErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		WriteErr(w, http.StatusBadRequest, "validation_error", "invalid input — check email, name, and password (min 8 chars)")
	case errors.Is(err, domain.ErrConflict):
		WriteErr(w, http.StatusConflict, "conflict", "email already registered")
	case errors.Is(err, domain.ErrInvalidCredentials):
		WriteErr(w, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect")
	case errors.Is(err, domain.ErrUnauthorized):
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "session invalid or expired — sign in again")
	case errors.Is(err, domain.ErrNotFound):
		WriteErr(w, http.StatusNotFound, "not_found", "resource not found")
	case errors.Is(err, domain.ErrForbidden):
		WriteErr(w, http.StatusForbidden, "forbidden", "you do not have permission to do that")
	case errors.Is(err, domain.ErrLLMUnavailable):
		WriteErr(w, http.StatusBadGateway, "llm_unavailable", "classification service is unavailable — try again")
	default:
		WriteErr(w, http.StatusInternalServerError, "internal_error", "something went wrong")
	}
}
