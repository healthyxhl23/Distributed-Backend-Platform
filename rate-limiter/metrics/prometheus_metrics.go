package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// RequestsTotal counts every rate limit check by algorithm and outcome.
	// Use this to monitor allow/deny ratios per algorithm.
	RequestsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "rate_limit_requests_total",
		Help: "Total number of rate limit checks by algorithm and result.",
	}, []string{"algorithm", "result"})

	// CheckDuration tracks how long each rate limit check takes.
	// Useful for detecting Redis latency spikes.
	CheckDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "rate_limit_check_duration_seconds",
		Help:    "Duration of rate limit check operations.",
		Buckets: prometheus.DefBuckets,
	}, []string{"algorithm"})

	// DeniedTotal counts denied requests per key prefix.
	// Use this to spot users or endpoints getting throttled most.
	DeniedTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "rate_limit_denied_total",
		Help: "Total number of denied requests by algorithm.",
	}, []string{"algorithm"})
)
