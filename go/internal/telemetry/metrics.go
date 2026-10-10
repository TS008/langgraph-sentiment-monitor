package telemetry

import (
	"net/http"
	"sync/atomic"
)

// Metrics holds Prometheus-friendly counters (text exposition without extra deps).
type Metrics struct {
	HTTPRequests   atomic.Uint64
	PipelineRuns   atomic.Uint64
	PipelineErrors atomic.Uint64
	LLMCalls       atomic.Uint64
	CrawlSignals   atomic.Uint64
}

func NewMetrics() *Metrics {
	return &Metrics{}
}

func (m *Metrics) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte("# HELP aegis_http_requests_total HTTP requests served\n"))
		_, _ = w.Write([]byte("# TYPE aegis_http_requests_total counter\n"))
		writeCounter(w, "aegis_http_requests_total", m.HTTPRequests.Load())
		_, _ = w.Write([]byte("# HELP aegis_pipeline_runs_total Pipeline runs completed\n"))
		_, _ = w.Write([]byte("# TYPE aegis_pipeline_runs_total counter\n"))
		writeCounter(w, "aegis_pipeline_runs_total", m.PipelineRuns.Load())
		_, _ = w.Write([]byte("# HELP aegis_pipeline_errors_total Pipeline runs with errors\n"))
		_, _ = w.Write([]byte("# TYPE aegis_pipeline_errors_total counter\n"))
		writeCounter(w, "aegis_pipeline_errors_total", m.PipelineErrors.Load())
		_, _ = w.Write([]byte("# HELP aegis_llm_calls_total LLM API calls\n"))
		_, _ = w.Write([]byte("# TYPE aegis_llm_calls_total counter\n"))
		writeCounter(w, "aegis_llm_calls_total", m.LLMCalls.Load())
		_, _ = w.Write([]byte("# HELP aegis_crawl_signals_total Signals ingested\n"))
		_, _ = w.Write([]byte("# TYPE aegis_crawl_signals_total counter\n"))
		writeCounter(w, "aegis_crawl_signals_total", m.CrawlSignals.Load())
	})
}

func writeCounter(w http.ResponseWriter, name string, v uint64) {
	_, _ = w.Write([]byte(name + " " + itoa(v) + "\n"))
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
