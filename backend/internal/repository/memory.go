package repository

import (
	"context"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

// MemoryStore is the local-dev fallback when DATABASE_URL is empty.
type MemoryStore struct {
	mu            sync.RWMutex
	usersByID     map[string]domain.User
	usersByEmail  map[string]string // email → id
	refreshByID   map[string]domain.RefreshToken
	refreshByHash map[string]string // hash → id

	reviewsByID     map[string]domain.Review
	classifications map[string][]domain.Classification // reviewID → runs
	breakdowns      map[string][]domain.ScoreBreakdownRow
}

// NewMemoryStore constructs an empty in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		usersByID:       make(map[string]domain.User),
		usersByEmail:    make(map[string]string),
		refreshByID:     make(map[string]domain.RefreshToken),
		refreshByHash:   make(map[string]string),
		reviewsByID:     make(map[string]domain.Review),
		classifications: make(map[string][]domain.Classification),
		breakdowns:      make(map[string][]domain.ScoreBreakdownRow),
	}
}

// Name identifies the store implementation for health responses.
func (m *MemoryStore) Name() string {
	return "memory"
}

// Ping always succeeds for the in-memory store.
func (m *MemoryStore) Ping(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (m *MemoryStore) CreateUser(ctx context.Context, user domain.User) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	email := strings.ToLower(strings.TrimSpace(user.Email))
	if _, exists := m.usersByEmail[email]; exists {
		return domain.User{}, domain.ErrConflict
	}
	user.Email = email
	m.usersByID[user.ID] = user
	m.usersByEmail[email] = user.ID
	return user, nil
}

func (m *MemoryStore) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	id, ok := m.usersByEmail[strings.ToLower(strings.TrimSpace(email))]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return m.usersByID[id], nil
}

func (m *MemoryStore) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	u, ok := m.usersByID[id]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}
	return u, nil
}

func (m *MemoryStore) UpdateUser(ctx context.Context, user domain.User) (domain.User, error) {
	if err := ctx.Err(); err != nil {
		return domain.User{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.usersByID[user.ID]
	if !ok {
		return domain.User{}, domain.ErrNotFound
	}

	newEmail := strings.ToLower(strings.TrimSpace(user.Email))
	if newEmail != existing.Email {
		if otherID, taken := m.usersByEmail[newEmail]; taken && otherID != user.ID {
			return domain.User{}, domain.ErrConflict
		}
		delete(m.usersByEmail, existing.Email)
		m.usersByEmail[newEmail] = user.ID
	}

	existing.Email = newEmail
	existing.Name = user.Name
	m.usersByID[user.ID] = existing
	return existing, nil
}

func (m *MemoryStore) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	u, ok := m.usersByID[userID]
	if !ok {
		return domain.ErrNotFound
	}
	u.PasswordHash = passwordHash
	m.usersByID[userID] = u
	return nil
}

func (m *MemoryStore) CreateRefreshToken(ctx context.Context, token domain.RefreshToken) (domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return domain.RefreshToken{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.refreshByID[token.ID] = token
	m.refreshByHash[token.TokenHash] = token.ID
	return token, nil
}

func (m *MemoryStore) GetRefreshTokenByHash(ctx context.Context, hash string) (domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return domain.RefreshToken{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	id, ok := m.refreshByHash[hash]
	if !ok {
		return domain.RefreshToken{}, domain.ErrNotFound
	}
	return m.refreshByID[id], nil
}

func (m *MemoryStore) RevokeRefreshToken(ctx context.Context, id string, at time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	t, ok := m.refreshByID[id]
	if !ok {
		return domain.ErrNotFound
	}
	if t.RevokedAt == nil {
		t.RevokedAt = &at
		m.refreshByID[id] = t
	}
	return nil
}

func (m *MemoryStore) RevokeUserRefreshTokens(ctx context.Context, userID string, at time.Time, exceptID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	for id, t := range m.refreshByID {
		if t.UserID != userID || id == exceptID {
			continue
		}
		if t.RevokedAt == nil {
			revoked := at
			t.RevokedAt = &revoked
			m.refreshByID[id] = t
		}
	}
	return nil
}

func (m *MemoryStore) ListUserRefreshTokens(ctx context.Context, userID string) ([]domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]domain.RefreshToken, 0)
	for _, t := range m.refreshByID {
		if t.UserID == userID {
			out = append(out, t)
		}
	}
	return out, nil
}

func (m *MemoryStore) ListUserGames(ctx context.Context, userID string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, r := range m.reviewsByID {
		if r.UserID != userID {
			continue
		}
		if _, ok := seen[r.GameName]; ok {
			continue
		}
		seen[r.GameName] = struct{}{}
		out = append(out, r.GameName)
	}
	sort.Strings(out)
	return out, nil
}

func (m *MemoryStore) CreateReview(ctx context.Context, review domain.Review, runs []domain.Classification, breakdown []domain.ScoreBreakdownRow) (domain.Review, error) {
	if err := ctx.Err(); err != nil {
		return domain.Review{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	m.reviewsByID[review.ID] = review
	cpRuns := make([]domain.Classification, len(runs))
	copy(cpRuns, runs)
	m.classifications[review.ID] = cpRuns
	cpBD := make([]domain.ScoreBreakdownRow, len(breakdown))
	copy(cpBD, breakdown)
	m.breakdowns[review.ID] = cpBD
	return review, nil
}

func (m *MemoryStore) GetReviewForUser(ctx context.Context, userID, reviewID string) (domain.Review, error) {
	if err := ctx.Err(); err != nil {
		return domain.Review{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	r, ok := m.reviewsByID[reviewID]
	if !ok || r.UserID != userID {
		return domain.Review{}, domain.ErrNotFound
	}
	return r, nil
}

func (m *MemoryStore) ListReviews(ctx context.Context, userID string, filter domain.ReviewListFilter) ([]domain.Review, int, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	all := make([]domain.Review, 0)
	for _, r := range m.reviewsByID {
		if r.UserID != userID {
			continue
		}
		if filter.Game != "" && !strings.EqualFold(r.GameName, filter.Game) {
			continue
		}
		if filter.Grade != "" && !strings.EqualFold(r.Grade, filter.Grade) {
			continue
		}
		if filter.NeedsReview != nil && r.NeedsReview != *filter.NeedsReview {
			continue
		}
		all = append(all, r)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt.After(all[j].CreatedAt)
	})

	total := len(all)
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset >= total {
		return []domain.Review{}, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return all[offset:end], total, nil
}

func (m *MemoryStore) DeleteReview(ctx context.Context, userID, reviewID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.reviewsByID[reviewID]
	if !ok || r.UserID != userID {
		return domain.ErrNotFound
	}
	delete(m.reviewsByID, reviewID)
	delete(m.classifications, reviewID)
	delete(m.breakdowns, reviewID)
	return nil
}

func (m *MemoryStore) GetClassifications(ctx context.Context, reviewID string) ([]domain.Classification, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	runs, ok := m.classifications[reviewID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	out := make([]domain.Classification, len(runs))
	copy(out, runs)
	return out, nil
}

func (m *MemoryStore) GetScoreBreakdown(ctx context.Context, reviewID string) ([]domain.ScoreBreakdownRow, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	bd, ok := m.breakdowns[reviewID]
	if !ok {
		return nil, domain.ErrNotFound
	}
	out := make([]domain.ScoreBreakdownRow, len(bd))
	copy(out, bd)
	return out, nil
}

func (m *MemoryStore) ReplaceScore(ctx context.Context, userID string, review domain.Review, breakdown []domain.ScoreBreakdownRow) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	existing, ok := m.reviewsByID[review.ID]
	if !ok || existing.UserID != userID {
		return domain.ErrNotFound
	}
	existing.TrustScore = review.TrustScore
	existing.Grade = review.Grade
	existing.NeedsReview = review.NeedsReview
	m.reviewsByID[review.ID] = existing
	cp := make([]domain.ScoreBreakdownRow, len(breakdown))
	copy(cp, breakdown)
	m.breakdowns[review.ID] = cp
	return nil
}

func (m *MemoryStore) ListAllReviewsForUser(ctx context.Context, userID string) ([]domain.Review, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]domain.Review, 0)
	for _, r := range m.reviewsByID {
		if r.UserID == userID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *MemoryStore) ListReviewsForGame(ctx context.Context, userID, gameName string) ([]domain.Review, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	out := make([]domain.Review, 0)
	for _, r := range m.reviewsByID {
		if r.UserID == userID && strings.EqualFold(r.GameName, gameName) {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *MemoryStore) UpsertGradeFeedback(ctx context.Context, userID, reviewID string, fb domain.ClassificationFeedback) (domain.Review, error) {
	if err := ctx.Err(); err != nil {
		return domain.Review{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	r, ok := m.reviewsByID[reviewID]
	if !ok || r.UserID != userID {
		return domain.Review{}, domain.ErrNotFound
	}
	cp := fb
	r.Feedback = &cp
	score, label := domain.CorrectnessFromFeedback(cp)
	r.CorrectnessScore = &score
	r.CorrectnessLabel = label
	m.reviewsByID[reviewID] = r
	return r, nil
}

func (m *MemoryStore) ListGradeFeedback(ctx context.Context, userID string, limit int) ([]domain.FeedbackHint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	if limit <= 0 {
		limit = 10
	}
	type pair struct {
		r  domain.Review
		at time.Time
	}
	rows := make([]pair, 0)
	for _, r := range m.reviewsByID {
		if r.UserID != userID || r.Feedback == nil {
			continue
		}
		rows = append(rows, pair{r: r, at: r.Feedback.CreatedAt})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].at.After(rows[j].at)
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}
	out := make([]domain.FeedbackHint, 0, len(rows))
	for _, row := range rows {
		fb := row.r.Feedback
		out = append(out, domain.FeedbackHint{
			GameName: row.r.GameName,
			Stars:    row.r.Stars,
			Note:     fb.Note,
			Corrections: []domain.DimCorrection{
				packCorrection("consistency", fb.Consistency),
				packCorrection("authenticity", fb.Authenticity),
				packCorrection("experience", fb.Experience),
				packCorrection("usefulness", fb.Usefulness),
			},
		})
	}
	return out, nil
}

func packCorrection(dim string, j domain.DimJudgment) domain.DimCorrection {
	return domain.DimCorrection{
		Dimension:    dim,
		ModelLabel:   j.ModelLabel,
		Correct:      j.Correct,
		CorrectLabel: j.CorrectLabel,
	}
}
