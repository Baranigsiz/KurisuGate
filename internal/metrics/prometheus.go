package metrics

import (
	"fmt"
	"net/http"
	"strings"
)

// PrometheusHandler exposes standard OpenTelemetry / Prometheus text format metrics
func (c *Collector) PrometheusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		snap := c.GetSnapshot()

		var b strings.Builder
		b.WriteString("# HELP kurisugate_uptime_seconds Total runtime of KurisuGate\n")
		b.WriteString("# TYPE kurisugate_uptime_seconds gauge\n")
		b.WriteString(fmt.Sprintf("kurisugate_uptime_seconds %.2f\n\n", snap.Uptime.Seconds()))

		b.WriteString("# HELP kurisugate_requests_total Total HTTP requests received\n")
		b.WriteString("# TYPE kurisugate_requests_total counter\n")
		b.WriteString(fmt.Sprintf("kurisugate_requests_total{type=\"all\"} %d\n", snap.TotalRequests))
		b.WriteString(fmt.Sprintf("kurisugate_requests_total{type=\"success\"} %d\n", snap.SuccessRequests))
		b.WriteString(fmt.Sprintf("kurisugate_requests_total{type=\"failed\"} %d\n\n", snap.FailedRequests))

		b.WriteString("# HELP kurisugate_cache_hits_total Total responses served by cache\n")
		b.WriteString("# TYPE kurisugate_cache_hits_total counter\n")
		b.WriteString(fmt.Sprintf("kurisugate_cache_hits_total{type=\"exact\"} %d\n", snap.ExactCacheHits))
		b.WriteString(fmt.Sprintf("kurisugate_cache_hits_total{type=\"semantic\"} %d\n\n", snap.SemanticCacheHits))

		b.WriteString("# HELP kurisugate_cost_saved_dollars Total estimated API cost saved by caching\n")
		b.WriteString("# TYPE kurisugate_cost_saved_dollars counter\n")
		b.WriteString(fmt.Sprintf("kurisugate_cost_saved_dollars %.6f\n\n", snap.TotalCostSaved))

		b.WriteString("# HELP kurisugate_cost_incurred_dollars Total estimated API cost incurred upstream\n")
		b.WriteString("# TYPE kurisugate_cost_incurred_dollars counter\n")
		b.WriteString(fmt.Sprintf("kurisugate_cost_incurred_dollars %.6f\n\n", snap.TotalCostIncurred))

		b.WriteString("# HELP kurisugate_avg_latency_ms Average request latency in milliseconds\n")
		b.WriteString("# TYPE kurisugate_avg_latency_ms gauge\n")
		b.WriteString(fmt.Sprintf("kurisugate_avg_latency_ms %.2f\n\n", snap.AvgLatencyMs))

		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(b.String()))
	}
}
