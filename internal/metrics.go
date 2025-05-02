package core

import (
	"github.com/prometheus/client_golang/prometheus"
)

var (
	HTTPRequestsTotal *prometheus.CounterVec
	HTTPDuration      *prometheus.HistogramVec
)

// InitMetrics registers core metrics collectors.
func InitMetrics() {
	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "gonads",
		Name:      "http_requests_total",
		Help:      "Total HTTP requests processed, labeled by method and route.",
	}, []string{"method", "route", "status"})

	HTTPDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "gonads",
		Name:      "http_request_duration_seconds",
		Help:      "Histogram of request durations.",
		Buckets:   prometheus.DefBuckets,
	}, []string{"method", "route"})

	prometheus.MustRegister(HTTPRequestsTotal, HTTPDuration)
}
