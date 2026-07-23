package metrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	httpRequests = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "reviewguard_http_requests_total",
		Help: "Total HTTP requests by method, route, and status.",
	}, []string{"method", "route", "status"})

	httpDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "reviewguard_http_request_duration_seconds",
		Help:    "HTTP request latency in seconds.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	ClassifyDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "reviewguard_classify_duration_seconds",
		Help:    "Time spent classifying a review via MLC LLM.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5, 10, 30},
	})

	ClassifyErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "reviewguard_classify_errors_total",
		Help: "Classification failures against the MLC LLM service.",
	})
)

// Handler serves Prometheus scrape endpoint.
func Handler() http.Handler {
	return promhttp.Handler()
}

// Middleware records request counts and latencies.
func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		route := routePattern(r)
		status := strconv.Itoa(ww.Status())
		httpRequests.WithLabelValues(r.Method, route, status).Inc()
		httpDuration.WithLabelValues(r.Method, route).Observe(time.Since(start).Seconds())
	})
}

func routePattern(r *http.Request) string {
	if rctx := chi.RouteContext(r.Context()); rctx != nil {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return "unknown"
}
