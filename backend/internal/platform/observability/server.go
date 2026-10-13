package observability

import "net/http"

func MetricsHandler(metrics *Metrics) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", metrics.Handler())
	return mux
}
