package httpapi

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"DomainHunter/internal/query"
)

// handleMetrics 返回无需认证的 Prometheus 文本格式指标。
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	counts, _, err := s.deps.Domains.StatusCounts(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, err.Error())
		return
	}
	var out strings.Builder
	out.WriteString("# TYPE domainhunter_domains_total gauge\n")
	for status, count := range counts {
		fmt.Fprintf(&out, "domainhunter_domains_total{status=%s} %d\n", strconv.Quote(string(status)), count)
	}

	var totalRequests uint64
	var totalDuration float64
	if s.deps.Engine != nil && s.deps.Engine.Metrics() != nil {
		totalRequests, totalDuration = s.deps.Engine.Metrics().Snapshot()
	}
	out.WriteString("# TYPE domainhunter_query_duration_seconds histogram\n")
	fmt.Fprintf(&out, "domainhunter_query_duration_seconds_count %d\n", totalRequests)
	fmt.Fprintf(&out, "domainhunter_query_duration_seconds_sum %g\n", totalDuration)
	if s.deps.Engine != nil && s.deps.Engine.Metrics() != nil {
		bounds := query.DurationBucketBounds()
		// Provider buckets are accumulated into the same application-wide
		// histogram so Prometheus receives valid cumulative counts.
		cumulative := make([]uint64, len(bounds))
		for _, metric := range s.deps.Engine.Metrics().ProviderSnapshot() {
			for i := range bounds {
				cumulative[i] += metric.Buckets[i]
			}
		}
		for i, bound := range bounds {
			fmt.Fprintf(&out, "domainhunter_query_duration_seconds_bucket{le=%s} %d\n", strconv.Quote(strconv.FormatFloat(bound, 'f', -1, 64)), cumulative[i])
		}
		fmt.Fprintf(&out, "domainhunter_query_duration_seconds_bucket{le=\"+Inf\"} %d\n", totalRequests)
	}
	out.WriteString("# TYPE domainhunter_query_errors_total counter\n")
	if s.deps.Engine != nil && s.deps.Engine.Metrics() != nil {
		for provider, metric := range s.deps.Engine.Metrics().ProviderSnapshot() {
			fmt.Fprintf(&out, "domainhunter_query_errors_total{provider=%s} %d\n", strconv.Quote(provider), metric.Errors)
		}
	}
	queueDepth := 0
	if s.deps.Monitor != nil && s.deps.Monitor.Scheduler() != nil {
		queueDepth = s.deps.Monitor.Scheduler().Queue().Size()
	}
	out.WriteString("# TYPE domainhunter_scheduler_queue_depth gauge\n")
	fmt.Fprintf(&out, "domainhunter_scheduler_queue_depth %d\n", queueDepth)

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(out.String()))
}
