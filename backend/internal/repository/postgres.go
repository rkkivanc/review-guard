package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/scoring"
)

// PostgresStore implements Store against PostgreSQL via pgxpool.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// OpenPostgresStore opens a pool, tunes connection limits, and runs migrations.
func OpenPostgresStore(ctx context.Context, databaseURL string) (*PostgresStore, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if cfg.MaxConns == 0 {
		cfg.MaxConns = 20
	}
	if cfg.MinConns == 0 {
		cfg.MinConns = 2
	}
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = 30 * time.Minute
	cfg.HealthCheckPeriod = time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	if err := Migrate(ctx, pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

func (p *PostgresStore) Name() string { return "postgres" }

func (p *PostgresStore) Close() error {
	p.pool.Close()
	return nil
}

func (p *PostgresStore) Ping(ctx context.Context) error {
	return p.pool.Ping(ctx)
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (p *PostgresStore) GetUserByEmail(ctx context.Context, email string) (domain.User, error) {
	return p.scanUser(ctx, `
		SELECT id, email, name, password_hash, token_version, created_at
		FROM users WHERE email=$1`, strings.ToLower(strings.TrimSpace(email)))
}

func (p *PostgresStore) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	return p.scanUser(ctx, `
		SELECT id, email, name, password_hash, token_version, created_at
		FROM users WHERE id=$1`, id)
}

func (p *PostgresStore) scanUser(ctx context.Context, q string, arg any) (domain.User, error) {
	var u domain.User
	err := p.pool.QueryRow(ctx, q, arg).Scan(&u.ID, &u.Email, &u.Name, &u.PasswordHash, &u.TokenVersion, &u.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	return u, err
}

func (p *PostgresStore) CreateUser(ctx context.Context, user domain.User) (domain.User, error) {
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	_, err := p.pool.Exec(ctx, `
		INSERT INTO users (id, email, name, password_hash, token_version, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		user.ID, user.Email, user.Name, user.PasswordHash, user.TokenVersion, user.CreatedAt)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.User{}, domain.ErrConflict
		}
		return domain.User{}, err
	}
	return user, nil
}

func (p *PostgresStore) UpdateUser(ctx context.Context, user domain.User) (domain.User, error) {
	user.Email = strings.ToLower(strings.TrimSpace(user.Email))
	tag, err := p.pool.Exec(ctx, `
		UPDATE users SET email=$2, name=$3 WHERE id=$1`,
		user.ID, user.Email, user.Name)
	if err != nil {
		if isUniqueViolation(err) {
			return domain.User{}, domain.ErrConflict
		}
		return domain.User{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.User{}, domain.ErrNotFound
	}
	return p.GetUserByID(ctx, user.ID)
}

func (p *PostgresStore) UpdatePassword(ctx context.Context, userID, passwordHash string) error {
	tag, err := p.pool.Exec(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, userID, passwordHash)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (p *PostgresStore) CreateRefreshToken(ctx context.Context, token domain.RefreshToken) (domain.RefreshToken, error) {
	_, err := p.pool.Exec(ctx, `
		INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at, revoked_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		token.ID, token.UserID, token.TokenHash, token.ExpiresAt, token.RevokedAt, token.CreatedAt)
	return token, err
}

func (p *PostgresStore) GetRefreshTokenByHash(ctx context.Context, hash string) (domain.RefreshToken, error) {
	var t domain.RefreshToken
	err := p.pool.QueryRow(ctx, `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM refresh_tokens WHERE token_hash=$1`, hash).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.RefreshToken{}, domain.ErrNotFound
	}
	return t, err
}

func (p *PostgresStore) ConsumeRefreshToken(ctx context.Context, hash string, at time.Time) (domain.RefreshToken, error) {
	var t domain.RefreshToken
	err := p.pool.QueryRow(ctx, `
		UPDATE refresh_tokens
		SET revoked_at=$2
		WHERE token_hash=$1 AND revoked_at IS NULL AND expires_at > $2
		RETURNING id, user_id, token_hash, expires_at, revoked_at, created_at`, hash, at).
		Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt)
	if err == nil {
		return t, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return domain.RefreshToken{}, err
	}
	// Token missing, already revoked, or expired.
	existing, getErr := p.GetRefreshTokenByHash(ctx, hash)
	if errors.Is(getErr, domain.ErrNotFound) {
		return domain.RefreshToken{}, domain.ErrNotFound
	}
	if getErr != nil {
		return domain.RefreshToken{}, getErr
	}
	return existing, domain.ErrTokenReuse
}

func (p *PostgresStore) BumpTokenVersion(ctx context.Context, userID string) (int64, error) {
	var v int64
	err := p.pool.QueryRow(ctx, `
		UPDATE users SET token_version = token_version + 1
		WHERE id=$1 RETURNING token_version`, userID).Scan(&v)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
	}
	return v, err
}

func (p *PostgresStore) RevokeRefreshToken(ctx context.Context, id string, at time.Time) error {
	tag, err := p.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at=$2
		WHERE id=$1 AND revoked_at IS NULL`, id, at)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		// Distinguish missing vs already revoked: check existence.
		var exists bool
		if err := p.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM refresh_tokens WHERE id=$1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return domain.ErrNotFound
		}
	}
	return nil
}

func (p *PostgresStore) RevokeUserRefreshTokens(ctx context.Context, userID string, at time.Time, exceptID string) error {
	if exceptID == "" {
		_, err := p.pool.Exec(ctx, `
			UPDATE refresh_tokens SET revoked_at=$2
			WHERE user_id=$1 AND revoked_at IS NULL`, userID, at)
		return err
	}
	_, err := p.pool.Exec(ctx, `
		UPDATE refresh_tokens SET revoked_at=$2
		WHERE user_id=$1 AND revoked_at IS NULL AND id<>$3`, userID, at, exceptID)
	return err
}

func (p *PostgresStore) ListUserRefreshTokens(ctx context.Context, userID string) ([]domain.RefreshToken, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, user_id, token_hash, expires_at, revoked_at, created_at
		FROM refresh_tokens WHERE user_id=$1 ORDER BY created_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.RefreshToken, 0)
	for rows.Next() {
		var t domain.RefreshToken
		if err := rows.Scan(&t.ID, &t.UserID, &t.TokenHash, &t.ExpiresAt, &t.RevokedAt, &t.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (p *PostgresStore) ListUserGames(ctx context.Context, userID string) ([]string, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT DISTINCT game_name FROM reviews WHERE user_id=$1 ORDER BY game_name`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0)
	for rows.Next() {
		var g string
		if err := rows.Scan(&g); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

func (p *PostgresStore) CreateReview(ctx context.Context, review domain.Review, runs []domain.Classification, breakdown []domain.ScoreBreakdownRow) (domain.Review, error) {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return domain.Review{}, err
	}
	defer tx.Rollback(ctx)

	fbJSON, err := marshalFeedback(review.Feedback)
	if err != nil {
		return domain.Review{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO reviews (
			id, user_id, game_name, stars, review_text, trust_score, grade, needs_review,
			latency_ms, created_at, feedback, correctness_score, correctness_label
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		review.ID, review.UserID, review.GameName, review.Stars, review.ReviewText,
		review.TrustScore, review.Grade, review.NeedsReview, review.LatencyMS, review.CreatedAt,
		fbJSON, review.CorrectnessScore, nullIfEmpty(review.CorrectnessLabel))
	if err != nil {
		return domain.Review{}, err
	}
	for _, run := range runs {
		payload, err := json.Marshal(run.Payload)
		if err != nil {
			return domain.Review{}, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO classifications (id, review_id, run_index, payload)
			VALUES ($1,$2,$3,$4)`, run.ID, run.ReviewID, run.RunIndex, payload); err != nil {
			return domain.Review{}, err
		}
	}
	for _, row := range breakdown {
		if _, err := tx.Exec(ctx, `
			INSERT INTO score_breakdown (id, review_id, dimension, final_label, agreement, avg_confidence, dim_score)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			row.ID, row.ReviewID, row.Dimension, row.FinalLabel, row.Agreement, row.AvgConfidence, row.DimScore); err != nil {
			return domain.Review{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Review{}, err
	}
	return review, nil
}

func (p *PostgresStore) GetReviewForUser(ctx context.Context, userID, reviewID string) (domain.Review, error) {
	return p.scanReview(ctx, `
		SELECT id, user_id, game_name, stars, review_text, trust_score, grade, needs_review,
		       latency_ms, created_at, feedback, correctness_score, correctness_label
		FROM reviews WHERE id=$1 AND user_id=$2`, reviewID, userID)
}

func (p *PostgresStore) scanReview(ctx context.Context, q string, args ...any) (domain.Review, error) {
	var r domain.Review
	var fbJSON []byte
	var corrLabel *string
	err := p.pool.QueryRow(ctx, q, args...).Scan(
		&r.ID, &r.UserID, &r.GameName, &r.Stars, &r.ReviewText, &r.TrustScore, &r.Grade, &r.NeedsReview,
		&r.LatencyMS, &r.CreatedAt, &fbJSON, &r.CorrectnessScore, &corrLabel,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Review{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Review{}, err
	}
	if err := unmarshalFeedback(fbJSON, &r); err != nil {
		return domain.Review{}, err
	}
	if corrLabel != nil {
		r.CorrectnessLabel = *corrLabel
	}
	return r, nil
}

func (p *PostgresStore) ListReviews(ctx context.Context, userID string, filter domain.ReviewListFilter) ([]domain.Review, int, error) {
	limit := filter.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	offset := filter.Offset
	if offset < 0 {
		offset = 0
	}
	if offset > 10_000 {
		offset = 10_000
	}

	where := []string{"user_id=$1"}
	args := []any{userID}
	argN := 2
	if filter.Game != "" {
		where = append(where, fmt.Sprintf("lower(game_name)=lower($%d)", argN))
		args = append(args, filter.Game)
		argN++
	}
	if filter.Grade != "" {
		where = append(where, fmt.Sprintf("grade=$%d", argN))
		args = append(args, strings.ToUpper(filter.Grade))
		argN++
	}
	if filter.NeedsReview != nil {
		where = append(where, fmt.Sprintf("needs_review=$%d", argN))
		args = append(args, *filter.NeedsReview)
		argN++
	}
	clause := strings.Join(where, " AND ")

	var total int
	if err := p.pool.QueryRow(ctx, `SELECT COUNT(*) FROM reviews WHERE `+clause, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	q := fmt.Sprintf(`
		SELECT id, user_id, game_name, stars, review_text, trust_score, grade, needs_review,
		       latency_ms, created_at, feedback, correctness_score, correctness_label
		FROM reviews WHERE %s
		ORDER BY created_at DESC
		LIMIT $%d OFFSET $%d`, clause, argN, argN+1)

	rows, err := p.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]domain.Review, 0, limit)
	for rows.Next() {
		r, err := scanReviewRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

type scannable interface {
	Scan(dest ...any) error
}

func scanReviewRow(row scannable) (domain.Review, error) {
	var r domain.Review
	var fbJSON []byte
	var corrLabel *string
	err := row.Scan(
		&r.ID, &r.UserID, &r.GameName, &r.Stars, &r.ReviewText, &r.TrustScore, &r.Grade, &r.NeedsReview,
		&r.LatencyMS, &r.CreatedAt, &fbJSON, &r.CorrectnessScore, &corrLabel,
	)
	if err != nil {
		return domain.Review{}, err
	}
	if err := unmarshalFeedback(fbJSON, &r); err != nil {
		return domain.Review{}, err
	}
	if corrLabel != nil {
		r.CorrectnessLabel = *corrLabel
	}
	return r, nil
}

func (p *PostgresStore) DeleteReview(ctx context.Context, userID, reviewID string) error {
	tag, err := p.pool.Exec(ctx, `DELETE FROM reviews WHERE id=$1 AND user_id=$2`, reviewID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (p *PostgresStore) GetClassifications(ctx context.Context, reviewID string) ([]domain.Classification, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, review_id, run_index, payload
		FROM classifications WHERE review_id=$1 ORDER BY run_index`, reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Classification, 0, 3)
	for rows.Next() {
		var c domain.Classification
		var payload []byte
		if err := rows.Scan(&c.ID, &c.ReviewID, &c.RunIndex, &payload); err != nil {
			return nil, err
		}
		var run scoring.Run
		if err := json.Unmarshal(payload, &run); err != nil {
			return nil, err
		}
		c.Payload = run
		out = append(out, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, domain.ErrNotFound
	}
	return out, nil
}

func (p *PostgresStore) GetScoreBreakdown(ctx context.Context, reviewID string) ([]domain.ScoreBreakdownRow, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, review_id, dimension, final_label, agreement, avg_confidence, dim_score
		FROM score_breakdown WHERE review_id=$1`, reviewID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ScoreBreakdownRow, 0, 4)
	for rows.Next() {
		var row domain.ScoreBreakdownRow
		if err := rows.Scan(&row.ID, &row.ReviewID, &row.Dimension, &row.FinalLabel, &row.Agreement, &row.AvgConfidence, &row.DimScore); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(out) == 0 {
		return nil, domain.ErrNotFound
	}
	return out, nil
}

func (p *PostgresStore) ReplaceScore(ctx context.Context, userID string, review domain.Review, breakdown []domain.ScoreBreakdownRow) error {
	tx, err := p.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	tag, err := tx.Exec(ctx, `
		UPDATE reviews SET trust_score=$3, grade=$4, needs_review=$5
		WHERE id=$1 AND user_id=$2`,
		review.ID, userID, review.TrustScore, review.Grade, review.NeedsReview)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `DELETE FROM score_breakdown WHERE review_id=$1`, review.ID); err != nil {
		return err
	}
	for _, row := range breakdown {
		if _, err := tx.Exec(ctx, `
			INSERT INTO score_breakdown (id, review_id, dimension, final_label, agreement, avg_confidence, dim_score)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			row.ID, row.ReviewID, row.Dimension, row.FinalLabel, row.Agreement, row.AvgConfidence, row.DimScore); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (p *PostgresStore) ListAllReviewsForUser(ctx context.Context, userID string) ([]domain.Review, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, user_id, game_name, stars, review_text, trust_score, grade, needs_review,
		       latency_ms, created_at, feedback, correctness_score, correctness_label
		FROM reviews WHERE user_id=$1`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Review, 0)
	for rows.Next() {
		r, err := scanReviewRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *PostgresStore) ListReviewsForGame(ctx context.Context, userID, gameName string) ([]domain.Review, error) {
	rows, err := p.pool.Query(ctx, `
		SELECT id, user_id, game_name, stars, review_text, trust_score, grade, needs_review,
		       latency_ms, created_at, feedback, correctness_score, correctness_label
		FROM reviews WHERE user_id=$1 AND lower(game_name)=lower($2)`, userID, gameName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Review, 0)
	for rows.Next() {
		r, err := scanReviewRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (p *PostgresStore) UpsertGradeFeedback(ctx context.Context, userID, reviewID string, fb domain.ClassificationFeedback) (domain.Review, error) {
	score, label := domain.CorrectnessFromFeedback(fb)
	fbJSON, err := json.Marshal(fb)
	if err != nil {
		return domain.Review{}, err
	}
	tag, err := p.pool.Exec(ctx, `
		UPDATE reviews
		SET feedback=$3, correctness_score=$4, correctness_label=$5
		WHERE id=$1 AND user_id=$2`, reviewID, userID, fbJSON, score, label)
	if err != nil {
		return domain.Review{}, err
	}
	if tag.RowsAffected() == 0 {
		return domain.Review{}, domain.ErrNotFound
	}
	return p.GetReviewForUser(ctx, userID, reviewID)
}

func (p *PostgresStore) ListGradeFeedback(ctx context.Context, userID string, limit int) ([]domain.FeedbackHint, error) {
	if limit <= 0 {
		limit = 10
	}
	rows, err := p.pool.Query(ctx, `
		SELECT game_name, stars, feedback
		FROM reviews
		WHERE user_id=$1 AND feedback IS NOT NULL
		ORDER BY (feedback->>'created_at') DESC NULLS LAST
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.FeedbackHint, 0, limit)
	for rows.Next() {
		var game string
		var stars int
		var fbJSON []byte
		if err := rows.Scan(&game, &stars, &fbJSON); err != nil {
			return nil, err
		}
		var fb domain.ClassificationFeedback
		if err := json.Unmarshal(fbJSON, &fb); err != nil {
			return nil, err
		}
		out = append(out, domain.FeedbackHint{
			GameName: game,
			Stars:    stars,
			Note:     fb.Note,
			Corrections: []domain.DimCorrection{
				packCorrection("consistency", fb.Consistency),
				packCorrection("authenticity", fb.Authenticity),
				packCorrection("experience", fb.Experience),
				packCorrection("usefulness", fb.Usefulness),
			},
		})
	}
	return out, rows.Err()
}

func (p *PostgresStore) BuildAnalytics(ctx context.Context, userID string, threshold float64) (domain.Analytics, error) {
	out := domain.Analytics{
		GradeCounts:   map[string]int{"A": 0, "B": 0, "C": 0, "D": 0, "F": 0},
		DimensionDist: map[string]map[string]int{},
		Threshold:     threshold,
	}

	var gradeA, gradeB, gradeC, gradeD, gradeF int
	err := p.pool.QueryRow(ctx, `
		SELECT
			COUNT(*)::int,
			COUNT(*) FILTER (WHERE needs_review)::int,
			COALESCE(AVG(trust_score), 0),
			COUNT(*) FILTER (WHERE correctness_score IS NOT NULL)::int,
			COALESCE(AVG(correctness_score) FILTER (WHERE correctness_score IS NOT NULL), 0),
			COUNT(*) FILTER (WHERE trust_score < $2)::int,
			COUNT(*) FILTER (WHERE grade='A')::int,
			COUNT(*) FILTER (WHERE grade='B')::int,
			COUNT(*) FILTER (WHERE grade='C')::int,
			COUNT(*) FILTER (WHERE grade='D')::int,
			COUNT(*) FILTER (WHERE grade='F')::int
		FROM reviews WHERE user_id=$1`, userID, threshold).Scan(
		&out.TotalReviews,
		&out.FlaggedCount,
		&out.AvgTrustScore,
		&out.FeedbackScoredCount,
		&out.AvgCorrectnessScore,
		&out.WouldFlagAtThreshold,
		&gradeA, &gradeB, &gradeC, &gradeD, &gradeF,
	)
	if err != nil {
		return domain.Analytics{}, err
	}
	out.GradeCounts["A"] = gradeA
	out.GradeCounts["B"] = gradeB
	out.GradeCounts["C"] = gradeC
	out.GradeCounts["D"] = gradeD
	out.GradeCounts["F"] = gradeF

	rows, err := p.pool.Query(ctx, `
		SELECT sb.dimension, sb.final_label, COUNT(*)::int
		FROM score_breakdown sb
		INNER JOIN reviews r ON r.id = sb.review_id
		WHERE r.user_id=$1
		GROUP BY sb.dimension, sb.final_label`, userID)
	if err != nil {
		return domain.Analytics{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var dim, label string
		var n int
		if err := rows.Scan(&dim, &label, &n); err != nil {
			return domain.Analytics{}, err
		}
		if out.DimensionDist[dim] == nil {
			out.DimensionDist[dim] = map[string]int{}
		}
		out.DimensionDist[dim][label] = n
	}
	return out, rows.Err()
}

func marshalFeedback(fb *domain.ClassificationFeedback) ([]byte, error) {
	if fb == nil {
		return nil, nil
	}
	return json.Marshal(fb)
}

func unmarshalFeedback(raw []byte, r *domain.Review) error {
	if len(raw) == 0 {
		return nil
	}
	var fb domain.ClassificationFeedback
	if err := json.Unmarshal(raw, &fb); err != nil {
		return err
	}
	r.Feedback = &fb
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
