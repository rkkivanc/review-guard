package httpapi

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/masterfabric/review-guard/mf-backend/internal/domain"
	"github.com/masterfabric/review-guard/mf-backend/internal/service"
)

// ReviewHandler serves review + scoring endpoints.
type ReviewHandler struct {
	svc *service.ReviewService
}

// NewReviewHandler constructs a ReviewHandler.
func NewReviewHandler(svc *service.ReviewService) *ReviewHandler {
	return &ReviewHandler{svc: svc}
}

type createReviewRequest struct {
	GameName   string `json:"game_name"`
	Stars      int    `json:"stars"`
	ReviewText string `json:"review_text"`
}

// Create handles POST /reviews — backend classifies via MLC LLM then scores.
func (h *ReviewHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createReviewRequest
	if err := decodeJSONLimited(r, &req, maxReviewBody); err != nil {
		WriteErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	detail, err := h.svc.Create(r.Context(), userID, service.CreateReviewInput{
		GameName:   req.GameName,
		Stars:      req.Stars,
		ReviewText: req.ReviewText,
	})
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusCreated, detail)
}

// List handles GET /reviews.
func (h *ReviewHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	q := r.URL.Query()
	filter := domain.ReviewListFilter{
		Game:   q.Get("game"),
		Grade:  q.Get("grade"),
		Limit:  clampLimit(queryInt(q.Get("limit"), 50), 50, service.MaxPageSize),
		Offset: clampOffset(queryInt(q.Get("offset"), 0), service.MaxOffset),
	}
	if v := q.Get("needs_review"); v != "" {
		b := strings.EqualFold(v, "true") || v == "1"
		filter.NeedsReview = &b
	}
	items, err := h.svc.List(r.Context(), userID, filter)
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, items)
}

// Get handles GET /reviews/{id}.
func (h *ReviewHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	detail, err := h.svc.Get(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, detail)
}

// Delete handles DELETE /reviews/{id}.
func (h *ReviewHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := h.svc.Delete(r.Context(), userID, chi.URLParam(r, "id")); err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, service.DeletedResponse{Deleted: true})
}

// Rescore handles POST /reviews/{id}/rescore.
func (h *ReviewHandler) Rescore(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	detail, err := h.svc.Rescore(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, detail)
}

// Score handles GET /reviews/{id}/score.
func (h *ReviewHandler) Score(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	score, err := h.svc.ScoreOnly(r.Context(), userID, chi.URLParam(r, "id"))
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, score)
}

// Analytics handles GET /reviews/analytics.
func (h *ReviewHandler) Analytics(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	threshold := queryFloat(r.URL.Query().Get("threshold"), 0)
	data, err := h.svc.Analytics(r.Context(), userID, threshold)
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, data)
}

// GameAverage handles GET /games/{name}/average.
func (h *ReviewHandler) GameAverage(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	avg, err := h.svc.GameAverage(r.Context(), userID, chi.URLParam(r, "name"))
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, avg)
}

type feedbackRequest struct {
	Consistency  domain.DimJudgment `json:"consistency"`
	Authenticity domain.DimJudgment `json:"authenticity"`
	Experience   domain.DimJudgment `json:"experience"`
	Usefulness   domain.DimJudgment `json:"usefulness"`
	Note         string             `json:"note"`
}

// Feedback handles POST /reviews/{id}/feedback.
func (h *ReviewHandler) Feedback(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req feedbackRequest
	if err := decodeJSONLimited(r, &req, maxReviewBody); err != nil {
		WriteErr(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return
	}
	review, err := h.svc.SubmitFeedback(r.Context(), userID, chi.URLParam(r, "id"), domain.ClassificationFeedback{
		Consistency:  req.Consistency,
		Authenticity: req.Authenticity,
		Experience:   req.Experience,
		Usefulness:   req.Usefulness,
		Note:         req.Note,
	})
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, review)
}

// FeedbackHints handles GET /reviews/feedback/hints.
func (h *ReviewHandler) FeedbackHints(w http.ResponseWriter, r *http.Request) {
	userID, ok := UserIDFromContext(r.Context())
	if !ok {
		WriteErr(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	hints, err := h.svc.FeedbackHints(r.Context(), userID, clampLimit(queryInt(r.URL.Query().Get("limit"), 8), 8, 20))
	if err != nil {
		mapAuthErr(w, err)
		return
	}
	WriteOK(w, http.StatusOK, hints)
}

func clampLimit(n, fallback, max int) int {
	if n <= 0 {
		return fallback
	}
	if n > max {
		return max
	}
	return n
}

func clampOffset(n, max int) int {
	if n < 0 {
		return 0
	}
	if n > max {
		return max
	}
	return n
}

func queryInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

func queryFloat(raw string, fallback float64) float64 {
	if raw == "" {
		return fallback
	}
	n, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return fallback
	}
	return n
}
