package repository

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
)

// MemoryStore is the local-dev fallback when DATABASE_URL is empty.
// When opened via OpenMemoryStore, state is saved under dataDir/store.json
// asynchronously (never while holding the write lock for disk I/O).
type MemoryStore struct {
	mu      sync.RWMutex
	dataDir string

	usersByID     map[string]domain.User
	usersByEmail  map[string]string // email → id
	refreshByID   map[string]domain.RefreshToken
	refreshByHash map[string]string   // hash → id
	refreshByUser map[string][]string // userID → token IDs

	reviewsByID     map[string]domain.Review
	reviewsByUser   map[string][]string                 // userID → review IDs
	classifications map[string][]domain.Classification  // reviewID → runs
	breakdowns      map[string][]domain.ScoreBreakdownRow

	// Async persist: coalesce signals; single worker writes outside the mutex.
	persistCh chan struct{}
	stopCh    chan struct{}
	doneCh    chan struct{}
	closeOnce sync.Once
}

// NewMemoryStore constructs an empty in-memory store (no persistence).
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		usersByID:       make(map[string]domain.User),
		usersByEmail:    make(map[string]string),
		refreshByID:     make(map[string]domain.RefreshToken),
		refreshByHash:   make(map[string]string),
		refreshByUser:   make(map[string][]string),
		reviewsByID:     make(map[string]domain.Review),
		reviewsByUser:   make(map[string][]string),
		classifications: make(map[string][]domain.Classification),
		breakdowns:      make(map[string][]domain.ScoreBreakdownRow),
	}
}

// Name identifies the store implementation for health responses.
func (m *MemoryStore) Name() string {
	if m.dataDir != "" {
		return "memory+file"
	}
	return "memory"
}

// Close flushes pending persistence and stops the background worker.
func (m *MemoryStore) Close() error {
	if m.stopCh == nil {
		return nil
	}
	var err error
	m.closeOnce.Do(func() {
		close(m.stopCh)
		<-m.doneCh
	})
	return err
}

// Ping always succeeds for the in-memory store unless the context is cancelled.
func (m *MemoryStore) Ping(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

func (m *MemoryStore) schedulePersist() {
	if m.persistCh == nil {
		return
	}
	select {
	case m.persistCh <- struct{}{}:
	default: // already queued — coalesce
	}
}

func (m *MemoryStore) addRefreshIndexLocked(userID, tokenID string) {
	m.refreshByUser[userID] = append(m.refreshByUser[userID], tokenID)
}

func (m *MemoryStore) addReviewIndexLocked(userID, reviewID string) {
	m.reviewsByUser[userID] = append(m.reviewsByUser[userID], reviewID)
}

func (m *MemoryStore) removeReviewIndexLocked(userID, reviewID string) {
	ids := m.reviewsByUser[userID]
	for i, id := range ids {
		if id == reviewID {
			m.reviewsByUser[userID] = append(ids[:i], ids[i+1:]...)
			return
		}
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
	user.Role = domain.NormalizeRole(user.Role)
	m.usersByID[user.ID] = user
	m.usersByEmail[email] = user.ID
	m.schedulePersist()
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
	if user.Role != "" {
		existing.Role = domain.NormalizeRole(user.Role)
	}
	m.usersByID[user.ID] = existing
	m.schedulePersist()
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
	m.schedulePersist()
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
	m.addRefreshIndexLocked(token.UserID, token.ID)
	m.schedulePersist()
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

// ConsumeRefreshToken atomically marks a valid refresh token revoked.
func (m *MemoryStore) ConsumeRefreshToken(ctx context.Context, hash string, at time.Time) (domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return domain.RefreshToken{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	id, ok := m.refreshByHash[hash]
	if !ok {
		return domain.RefreshToken{}, domain.ErrNotFound
	}
	t := m.refreshByID[id]
	if t.RevokedAt != nil || at.After(t.ExpiresAt) {
		m.schedulePersist()
		return t, domain.ErrTokenReuse
	}
	revoked := at
	t.RevokedAt = &revoked
	m.refreshByID[id] = t
	m.schedulePersist()
	return t, nil
}

func (m *MemoryStore) BumpTokenVersion(ctx context.Context, userID string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	u, ok := m.usersByID[userID]
	if !ok {
		return 0, domain.ErrNotFound
	}
	u.TokenVersion++
	m.usersByID[userID] = u
	m.schedulePersist()
	return u.TokenVersion, nil
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
		m.schedulePersist()
	}
	return nil
}

func (m *MemoryStore) RevokeUserRefreshTokens(ctx context.Context, userID string, at time.Time, exceptID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	changed := false
	for _, id := range m.refreshByUser[userID] {
		if id == exceptID {
			continue
		}
		t, ok := m.refreshByID[id]
		if !ok || t.RevokedAt != nil {
			continue
		}
		revoked := at
		t.RevokedAt = &revoked
		m.refreshByID[id] = t
		changed = true
	}
	if changed {
		m.schedulePersist()
	}
	return nil
}

func (m *MemoryStore) ListUserRefreshTokens(ctx context.Context, userID string) ([]domain.RefreshToken, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := m.refreshByUser[userID]
	out := make([]domain.RefreshToken, 0, len(ids))
	for _, id := range ids {
		if t, ok := m.refreshByID[id]; ok {
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

	ids := m.reviewsByUser[userID]
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0)
	for _, id := range ids {
		r, ok := m.reviewsByID[id]
		if !ok {
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
	m.addReviewIndexLocked(review.UserID, review.ID)
	cpRuns := make([]domain.Classification, len(runs))
	copy(cpRuns, runs)
	m.classifications[review.ID] = cpRuns
	cpBD := make([]domain.ScoreBreakdownRow, len(breakdown))
	copy(cpBD, breakdown)
	m.breakdowns[review.ID] = cpBD
	m.schedulePersist()
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

	ids := m.reviewsByUser[userID]
	all := make([]domain.Review, 0, len(ids))
	for _, id := range ids {
		r, ok := m.reviewsByID[id]
		if !ok {
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
	out := make([]domain.Review, end-offset)
	copy(out, all[offset:end])
	return out, total, nil
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
	m.removeReviewIndexLocked(userID, reviewID)
	m.schedulePersist()
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
	m.schedulePersist()
	return nil
}

func (m *MemoryStore) ListAllReviewsForUser(ctx context.Context, userID string) ([]domain.Review, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := m.reviewsByUser[userID]
	out := make([]domain.Review, 0, len(ids))
	for _, id := range ids {
		if r, ok := m.reviewsByID[id]; ok {
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

	ids := m.reviewsByUser[userID]
	out := make([]domain.Review, 0, len(ids))
	for _, id := range ids {
		r, ok := m.reviewsByID[id]
		if !ok || !strings.EqualFold(r.GameName, gameName) {
			continue
		}
		out = append(out, r)
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
	m.schedulePersist()
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
	ids := m.reviewsByUser[userID]
	rows := make([]pair, 0, len(ids))
	for _, id := range ids {
		r, ok := m.reviewsByID[id]
		if !ok || r.Feedback == nil {
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

// BuildAnalytics aggregates dashboard metrics in a single lock pass (no N+1).
func (m *MemoryStore) BuildAnalytics(ctx context.Context, userID string, threshold float64) (domain.Analytics, error) {
	if err := ctx.Err(); err != nil {
		return domain.Analytics{}, err
	}
	m.mu.RLock()
	defer m.mu.RUnlock()

	ids := m.reviewsByUser[userID]
	out := domain.Analytics{
		TotalReviews:  len(ids),
		GradeCounts:   map[string]int{"A": 0, "B": 0, "C": 0, "D": 0, "F": 0},
		DimensionDist: map[string]map[string]int{},
		Threshold:     threshold,
	}

	var trustSum, corrSum float64
	counted := 0
	for _, id := range ids {
		r, ok := m.reviewsByID[id]
		if !ok {
			continue
		}
		counted++
		trustSum += r.TrustScore
		if r.NeedsReview {
			out.FlaggedCount++
		}
		if r.CorrectnessScore != nil {
			out.FeedbackScoredCount++
			corrSum += *r.CorrectnessScore
		}
		out.GradeCounts[r.Grade]++
		if r.TrustScore < threshold {
			out.WouldFlagAtThreshold++
		}
		for _, row := range m.breakdowns[id] {
			if out.DimensionDist[row.Dimension] == nil {
				out.DimensionDist[row.Dimension] = map[string]int{}
			}
			out.DimensionDist[row.Dimension][row.FinalLabel]++
		}
	}
	out.TotalReviews = counted
	if counted > 0 {
		out.AvgTrustScore = trustSum / float64(counted)
	}
	if out.FeedbackScoredCount > 0 {
		out.AvgCorrectnessScore = corrSum / float64(out.FeedbackScoredCount)
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

// ensure indexes exist after load (used by persist load path).
func (m *MemoryStore) rebuildIndexesLocked() {
	m.refreshByUser = make(map[string][]string, len(m.usersByID))
	for id, t := range m.refreshByID {
		m.refreshByUser[t.UserID] = append(m.refreshByUser[t.UserID], id)
	}
	m.reviewsByUser = make(map[string][]string, len(m.usersByID))
	for id, r := range m.reviewsByID {
		m.reviewsByUser[r.UserID] = append(m.reviewsByUser[r.UserID], id)
	}
}

// logPersistErr is used by the background worker.
func logPersistErr(err error) {
	if err != nil {
		slog.Error("memory store persist failed", "err", err)
	}
}
