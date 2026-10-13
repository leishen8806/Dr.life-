package observability

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Metrics struct {
	registry      *prometheus.Registry
	requests      *prometheus.CounterVec
	duration      *prometheus.HistogramVec
	errors        *prometheus.CounterVec
	inFlight      *prometheus.GaugeVec
	rateLimitHits *prometheus.CounterVec
	authFailures  *prometheus.CounterVec
	slowQueries   *prometheus.CounterVec
	business      *Business
}

func NewMetrics() *Metrics {
	registry := prometheus.NewRegistry()
	labels := []string{"role", "route", "method", "status_class"}
	m := &Metrics{
		registry: registry,
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "drlife_http_requests_total", Help: "HTTP requests."}, labels),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "drlife_http_request_duration_seconds", Help: "HTTP request latency.",
			Buckets: []float64{.005, .01, .025, .05, .1, .2, .3, .5, .75, 1, 2, 5, 10},
		}, labels),
		errors:        prometheus.NewCounterVec(prometheus.CounterOpts{Name: "drlife_http_errors_total", Help: "HTTP error responses."}, labels),
		inFlight:      prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "drlife_http_requests_in_flight", Help: "HTTP requests currently running."}, []string{"role"}),
		rateLimitHits: prometheus.NewCounterVec(prometheus.CounterOpts{Name: "drlife_gateway_rate_limit_hits_total", Help: "Rate-limit rejections."}, []string{"rule"}),
		authFailures:  prometheus.NewCounterVec(prometheus.CounterOpts{Name: "drlife_gateway_auth_failures_total", Help: "Authentication and authorization failures."}, []string{"status"}),
		slowQueries:   prometheus.NewCounterVec(prometheus.CounterOpts{Name: "drlife_db_slow_queries_total", Help: "Queries above the configured latency threshold."}, []string{"fingerprint"}),
	}
	m.business = newBusiness(registry)
	registry.MustRegister(
		m.requests, m.duration, m.errors, m.inFlight, m.rateLimitHits, m.authFailures, m.slowQueries,
		prometheus.NewGoCollector(), prometheus.NewProcessCollector(prometheus.ProcessCollectorOpts{}),
	)
	return m
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.registry, promhttp.HandlerOpts{})
}

func (m *Metrics) Business() *Business { return m.business }

func (m *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	if m == nil {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		role := roleForPath(r.URL.Path)
		m.inFlight.WithLabelValues(role).Inc()
		defer m.inFlight.WithLabelValues(role).Dec()

		captured := &statusCapturer{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(captured, r)
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		class := statusClass(captured.status)
		values := []string{role, route, methodLabel(r.Method), class}
		m.requests.WithLabelValues(values...).Inc()
		m.duration.WithLabelValues(values...).Observe(time.Since(started).Seconds())
		if captured.status >= 400 {
			m.errors.WithLabelValues(values...).Inc()
		}
		if captured.status == http.StatusTooManyRequests {
			m.rateLimitHits.WithLabelValues(ruleForPath(r.URL.Path)).Inc()
		}
		if captured.status == http.StatusUnauthorized || captured.status == http.StatusForbidden {
			m.authFailures.WithLabelValues(strconv.Itoa(captured.status)).Inc()
		}
	})
}

func methodLabel(method string) string {
	switch strings.ToUpper(method) {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions, http.MethodHead:
		return strings.ToUpper(method)
	default:
		return "OTHER"
	}
}

func statusClass(status int) string {
	switch status / 100 {
	case 1:
		return "1xx"
	case 2:
		return "2xx"
	case 3:
		return "3xx"
	case 4:
		return "4xx"
	case 5:
		return "5xx"
	default:
		return "other"
	}
}

func (m *Metrics) RegisterPool(pool *pgxpool.Pool) {
	if m == nil || pool == nil {
		return
	}
	m.registry.MustRegister(
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "drlife_db_pool_acquired_connections", Help: "Connections currently acquired."}, func() float64 { return float64(pool.Stat().AcquiredConns()) }),
		prometheus.NewGaugeFunc(prometheus.GaugeOpts{Name: "drlife_db_pool_idle_connections", Help: "Connections currently idle."}, func() float64 { return float64(pool.Stat().IdleConns()) }),
		prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "drlife_db_pool_acquire_wait_seconds_total", Help: "Cumulative time waiting to acquire a connection."}, func() float64 { return pool.Stat().AcquireDuration().Seconds() }),
	)
}

func roleForPath(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "v1" {
		switch parts[1] {
		case "buyer", "merchant", "driver", "wms", "admin", "public":
			return parts[1]
		}
	}
	return "other"
}

func ruleForPath(path string) string {
	for _, part := range strings.Split(strings.Trim(path, "/"), "/") {
		switch part {
		case "auth", "login":
			return "login"
		case "withdraw", "withdrawals":
			return "withdrawal"
		case "ai":
			return "ai"
		}
	}
	return "default"
}

type statusCapturer struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusCapturer) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status, w.wroteHeader = status, true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusCapturer) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *statusCapturer) Unwrap() http.ResponseWriter { return w.ResponseWriter }
