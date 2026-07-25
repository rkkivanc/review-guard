package service

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/masterfabric/review-guard/mf-backend/internal/auth"
	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

const (
	minPasswordLen = 8
	maxPasswordLen = 72 // bcrypt hard limit
	maxNameLen     = 100
	maxEmailLen    = 254
	bcryptCost     = bcrypt.DefaultCost
)

// registerDummyHash is a real bcrypt hash used only for timing padding.
var registerDummyHash []byte

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("timing-pad-not-a-real-password"), bcryptCost)
	if err != nil {
		panic("bcrypt dummy hash: " + err.Error())
	}
	registerDummyHash = h
}
// Register creates a user and returns a fresh session.
func (s *AuthService) Register(ctx context.Context, email, name, password string) (domain.AuthTokens, error) {
	email = normalizeEmail(email)
	name = strings.TrimSpace(name)
	if err := validateCredentials(email, name, password); err != nil {
		return domain.AuthTokens{}, err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return domain.AuthTokens{}, err
	}

	role := domain.RoleUser
	if s.isAdminEmail(email) {
		role = domain.RoleAdmin
	}
	user := domain.User{
		ID:           uuid.NewString(),
		Email:        email,
		Name:         name,
		Role:         role,
		PasswordHash: string(hash),
		TokenVersion: 0,
		CreatedAt:    s.now().UTC(),
	}
	created, err := s.store.CreateUser(ctx, user)
	if err != nil {
		if errors.Is(err, domain.ErrConflict) {
			// Mitigate email enumeration + timing oracle.
			_ = bcrypt.CompareHashAndPassword(registerDummyHash, []byte(password))
			slog.Info("register conflict suppressed", "email_domain", emailDomain(email))
			return domain.AuthTokens{}, domain.ErrValidation
		}
		return domain.AuthTokens{}, err
	}
	return s.issueSession(ctx, created)
}

// Login verifies credentials and returns a fresh session.
func (s *AuthService) Login(ctx context.Context, email, password string) (domain.AuthTokens, error) {
	email = normalizeEmail(email)
	if err := validatePasswordLen(password); err != nil || email == "" {
		return domain.AuthTokens{}, domain.ErrValidation
	}

	user, err := s.store.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			_ = bcrypt.CompareHashAndPassword(registerDummyHash, []byte(password))
			return domain.AuthTokens{}, domain.ErrInvalidCredentials
		}
		return domain.AuthTokens{}, err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return domain.AuthTokens{}, domain.ErrInvalidCredentials
	}
	return s.issueSession(ctx, user)
}

// Refresh rotates the refresh token and issues a new access token.
func (s *AuthService) Refresh(ctx context.Context, refreshRaw string) (domain.AuthTokens, error) {
	refreshRaw = strings.TrimSpace(refreshRaw)
	if refreshRaw == "" {
		return domain.AuthTokens{}, domain.ErrUnauthorized
	}

	now := s.now().UTC()
	stored, err := s.store.ConsumeRefreshToken(ctx, auth.HashRefreshToken(refreshRaw), now)
	if err != nil {
		if errors.Is(err, domain.ErrTokenReuse) {
			_ = s.store.RevokeUserRefreshTokens(ctx, stored.UserID, now, "")
			_, _ = s.store.BumpTokenVersion(ctx, stored.UserID)
			return domain.AuthTokens{}, domain.ErrUnauthorized
		}
		if errors.Is(err, domain.ErrNotFound) {
			return domain.AuthTokens{}, domain.ErrUnauthorized
		}
		return domain.AuthTokens{}, err
	}

	user, err := s.store.GetUserByID(ctx, stored.UserID)
	if err != nil {
		return domain.AuthTokens{}, err
	}
	return s.issueSession(ctx, user)
}

// Logout revokes the presented refresh token.
func (s *AuthService) Logout(ctx context.Context, refreshRaw string) error {
	refreshRaw = strings.TrimSpace(refreshRaw)
	if refreshRaw == "" {
		return domain.ErrValidation
	}
	stored, err := s.store.GetRefreshTokenByHash(ctx, auth.HashRefreshToken(refreshRaw))
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return nil // idempotent
		}
		return err
	}
	return s.store.RevokeRefreshToken(ctx, stored.ID, s.now().UTC())
}

// Me returns the current user profile.
func (s *AuthService) Me(ctx context.Context, userID string) (domain.User, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}
	user = s.normalizeUserRole(ctx, user)
	user.PasswordHash = ""
	return user, nil
}

// UpdateMe updates name and/or email.
func (s *AuthService) UpdateMe(ctx context.Context, userID, email, name string) (domain.User, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return domain.User{}, err
	}
	if email != "" {
		user.Email = normalizeEmail(email)
		if user.Email == "" || !strings.Contains(user.Email, "@") || utf8.RuneCountInString(user.Email) > maxEmailLen {
			return domain.User{}, domain.ErrValidation
		}
	}
	if name != "" {
		user.Name = strings.TrimSpace(name)
		if user.Name == "" || utf8.RuneCountInString(user.Name) > maxNameLen {
			return domain.User{}, domain.ErrValidation
		}
	}
	return s.store.UpdateUser(ctx, user)
}

// ChangePassword updates the password, bumps token version, and revokes other sessions.
func (s *AuthService) ChangePassword(ctx context.Context, userID, currentPassword, newPassword, currentRefreshRaw string) error {
	if err := validatePasswordLen(newPassword); err != nil {
		return err
	}
	if err := validatePasswordLen(currentPassword); err != nil {
		return domain.ErrInvalidCredentials
	}
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return err
	}
	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(currentPassword)); err != nil {
		return domain.ErrInvalidCredentials
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcryptCost)
	if err != nil {
		return err
	}
	if err := s.store.UpdatePassword(ctx, userID, string(hash)); err != nil {
		return err
	}

	// Invalidate all outstanding access JWTs.
	if _, err := s.store.BumpTokenVersion(ctx, userID); err != nil {
		return err
	}

	exceptID := ""
	if currentRefreshRaw != "" {
		if stored, err := s.store.GetRefreshTokenByHash(ctx, auth.HashRefreshToken(currentRefreshRaw)); err == nil {
			if stored.UserID == userID {
				exceptID = stored.ID
			}
		}
	}
	return s.store.RevokeUserRefreshTokens(ctx, userID, s.now().UTC(), exceptID)
}

// ListSessions returns active (non-revoked, non-expired) sessions for the user.
func (s *AuthService) ListSessions(ctx context.Context, userID, currentRefreshRaw string) ([]domain.SessionView, error) {
	tokens, err := s.store.ListUserRefreshTokens(ctx, userID)
	if err != nil {
		return nil, err
	}
	currentHash := ""
	if currentRefreshRaw != "" {
		currentHash = auth.HashRefreshToken(currentRefreshRaw)
	}
	now := s.now().UTC()
	out := make([]domain.SessionView, 0, len(tokens))
	for _, t := range tokens {
		if t.RevokedAt != nil || now.After(t.ExpiresAt) {
			continue
		}
		out = append(out, domain.SessionView{
			ID:        t.ID,
			ExpiresAt: t.ExpiresAt,
			RevokedAt: t.RevokedAt,
			CreatedAt: t.CreatedAt,
			Current:   currentHash != "" && t.TokenHash == currentHash,
		})
	}
	return out, nil
}

// ListGames returns distinct game names the user has reviewed (empty until reviews exist).
func (s *AuthService) ListGames(ctx context.Context, userID string) ([]string, error) {
	return s.store.ListUserGames(ctx, userID)
}

// ValidateAccessClaims ensures the user still exists and token_version matches.
func (s *AuthService) ValidateAccessClaims(ctx context.Context, userID string, tokenVersion int64) error {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrUnauthorized
		}
		return err
	}
	if user.TokenVersion != tokenVersion {
		return domain.ErrUnauthorized
	}
	return nil
}

func (s *AuthService) issueSession(ctx context.Context, user domain.User) (domain.AuthTokens, error) {
	user = s.normalizeUserRole(ctx, user)
	now := s.now().UTC()
	access, err := s.tokens.IssueAccessToken(user.ID, user.Email, user.TokenVersion, now)
	if err != nil {
		return domain.AuthTokens{}, err
	}
	raw, hash, err := s.tokens.NewRefreshToken()
	if err != nil {
		return domain.AuthTokens{}, err
	}
	rt := domain.RefreshToken{
		ID:        uuid.NewString(),
		UserID:    user.ID,
		TokenHash: hash,
		ExpiresAt: now.Add(s.tokens.RefreshTTL()),
		CreatedAt: now,
	}
	if _, err := s.store.CreateRefreshToken(ctx, rt); err != nil {
		return domain.AuthTokens{}, err
	}

	user.PasswordHash = ""
	user.Role = domain.NormalizeRole(user.Role)
	return domain.AuthTokens{
		AccessToken:  access,
		RefreshToken: raw,
		TokenType:    "Bearer",
		ExpiresIn:    int64(s.tokens.AccessTTL().Seconds()),
		User:         user,
	}, nil
}

func (s *AuthService) normalizeUserRole(ctx context.Context, user domain.User) domain.User {
	desired := domain.NormalizeRole(user.Role)
	if s.isAdminEmail(user.Email) {
		desired = domain.RoleAdmin
	}
	if desired == domain.NormalizeRole(user.Role) {
		user.Role = desired
		return user
	}
	user.Role = desired
	if updated, err := s.store.UpdateUser(ctx, user); err == nil {
		updated.PasswordHash = ""
		return updated
	}
	return user
}

// IsAdmin reports whether the user has the admin role.
func (s *AuthService) IsAdmin(ctx context.Context, userID string) (bool, error) {
	user, err := s.store.GetUserByID(ctx, userID)
	if err != nil {
		return false, err
	}
	user = s.normalizeUserRole(ctx, user)
	return user.Role == domain.RoleAdmin, nil
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func emailDomain(email string) string {
	if i := strings.LastIndex(email, "@"); i >= 0 && i+1 < len(email) {
		return email[i+1:]
	}
	return ""
}

func validatePasswordLen(password string) error {
	if len(password) < minPasswordLen || len(password) > maxPasswordLen {
		return domain.ErrValidation
	}
	return nil
}

func validateCredentials(email, name, password string) error {
	if email == "" || !strings.Contains(email, "@") || utf8.RuneCountInString(email) > maxEmailLen {
		return domain.ErrValidation
	}
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		return domain.ErrValidation
	}
	return validatePasswordLen(password)
}
