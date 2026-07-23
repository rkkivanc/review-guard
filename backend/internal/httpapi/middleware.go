package httpapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/masterfabric/review-guard/mf-backend/internal/auth"
	"github.com/masterfabric/review-guard/mf-backend/internal/service"
)

type ctxKey string

const userIDKey ctxKey = "userID"

// UserIDFromContext returns the authenticated user id, if any.
func UserIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(userIDKey).(string)
	return id, ok && id != ""
}

// RequireAuth validates a Bearer access JWT, checks token_version, and injects user id.
func RequireAuth(tm *auth.TokenManager, authSvc *service.AuthService) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" || !strings.HasPrefix(strings.ToLower(header), "bearer ") {
				WriteErr(w, http.StatusUnauthorized, "unauthorized", "missing or invalid authorization header")
				return
			}
			raw := strings.TrimSpace(header[7:])
			claims, err := tm.ParseAccessToken(raw)
			if err != nil || claims.UserID == "" {
				WriteErr(w, http.StatusUnauthorized, "unauthorized", "invalid or expired access token")
				return
			}
			if err := authSvc.ValidateAccessClaims(r.Context(), claims.UserID, claims.TokenVersion); err != nil {
				WriteErr(w, http.StatusUnauthorized, "unauthorized", "invalid or expired access token")
				return
			}
			ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
