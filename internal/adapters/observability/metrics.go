package observability

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
)

const (
	AttachmentWorkerActivatedTotal = "dialog_attachment_worker_activated_total"
	AttachmentWorkerDeletedTotal   = "dialog_attachment_worker_deleted_total"
	AttachmentWorkerFailedTotal    = "dialog_attachment_worker_failed_total"
	AttachmentWorkerErrorsTotal    = "dialog_attachment_worker_errors_total"
	OutboxPublishedTotal           = "dialog_outbox_published_total"
	OutboxFailedTotal              = "dialog_outbox_failed_total"
	OutboxWorkerErrorsTotal        = "dialog_outbox_worker_errors_total"
	TeacherRequestedV2Total        = "dialog_teacher_requested_v2_total"
	TicketCleanupDeletedTotal      = "dialog_ticket_cleanup_deleted_total"
	TicketCleanupErrorsTotal       = "dialog_ticket_cleanup_errors_total"
)

var defaultCounterNames = []string{
	AttachmentWorkerActivatedTotal,
	AttachmentWorkerDeletedTotal,
	AttachmentWorkerFailedTotal,
	AttachmentWorkerErrorsTotal,
	OutboxPublishedTotal,
	OutboxFailedTotal,
	OutboxWorkerErrorsTotal,
	TeacherRequestedV2Total,
	TicketCleanupDeletedTotal,
	TicketCleanupErrorsTotal,
}

type httpKey struct {
	Method string
	Route  string
	Status int
}

type httpValue struct {
	Count    uint64
	Duration time.Duration
}

type Metrics struct {
	mu                   sync.Mutex
	http                 map[httpKey]httpValue
	counters             map[string]uint64
	websocketConnections atomic.Int64
}

func NewMetrics() *Metrics {
	metrics := &Metrics{
		http:     make(map[httpKey]httpValue),
		counters: make(map[string]uint64, len(defaultCounterNames)),
	}
	for _, name := range defaultCounterNames {
		metrics.counters[name] = 0
	}
	return metrics
}

func (m *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A recorder that does not implement http.Hijacker breaks a WebSocket
		// upgrade. WebSocket connections are instrumented by their handler.
		if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			next.ServeHTTP(w, r)
			return
		}
		recorder := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		started := time.Now()
		next.ServeHTTP(recorder, r)
		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unmatched"
		}
		key := httpKey{Method: r.Method, Route: route, Status: recorder.status}
		m.mu.Lock()
		value := m.http[key]
		value.Count++
		value.Duration += time.Since(started)
		m.http[key] = value
		m.mu.Unlock()
	})
}

func (m *Metrics) Increment(name string, delta int) {
	if m == nil || delta <= 0 || !validMetricName(name) {
		return
	}
	m.mu.Lock()
	m.counters[name] += uint64(delta)
	m.mu.Unlock()
}

func (m *Metrics) WebSocketOpened() {
	if m != nil {
		m.websocketConnections.Add(1)
	}
}

func (m *Metrics) WebSocketClosed() {
	if m != nil {
		m.websocketConnections.Add(-1)
	}
}

func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	m.mu.Lock()
	keys := make([]httpKey, 0, len(m.http))
	snapshot := make(map[httpKey]httpValue, len(m.http))
	for key, value := range m.http {
		keys = append(keys, key)
		snapshot[key] = value
	}
	counterNames := make([]string, 0, len(m.counters))
	counters := make(map[string]uint64, len(m.counters))
	for name, value := range m.counters {
		counterNames = append(counterNames, name)
		counters[name] = value
	}
	m.mu.Unlock()
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Route != keys[j].Route {
			return keys[i].Route < keys[j].Route
		}
		if keys[i].Method != keys[j].Method {
			return keys[i].Method < keys[j].Method
		}
		return keys[i].Status < keys[j].Status
	})
	sort.Strings(counterNames)
	_, _ = fmt.Fprintln(w, "# TYPE dialog_http_requests_total counter")
	_, _ = fmt.Fprintln(w, "# TYPE dialog_http_request_duration_seconds summary")
	for _, key := range keys {
		value := snapshot[key]
		labels := `method="` + escape(key.Method) + `",route="` + escape(key.Route) + `",status="` + strconv.Itoa(key.Status) + `"`
		_, _ = fmt.Fprintf(w, "dialog_http_requests_total{%s} %d\n", labels, value.Count)
		_, _ = fmt.Fprintf(w, "dialog_http_request_duration_seconds_sum{%s} %.6f\n", labels, value.Duration.Seconds())
		_, _ = fmt.Fprintf(w, "dialog_http_request_duration_seconds_count{%s} %d\n", labels, value.Count)
	}
	_, _ = fmt.Fprintln(w, "# TYPE dialog_websocket_connections gauge")
	_, _ = fmt.Fprintf(w, "dialog_websocket_connections %d\n", m.websocketConnections.Load())
	for _, name := range counterNames {
		_, _ = fmt.Fprintf(w, "# TYPE %s counter\n%s %d\n", name, name, counters[name])
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.wroteHeader {
		return
	}
	r.status = status
	r.wroteHeader = true
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(payload []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	return r.ResponseWriter.Write(payload)
}

func escape(value string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value)
}

func validMetricName(value string) bool {
	if value == "" {
		return false
	}
	for index, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') || character == '_' || character == ':' || (index > 0 && character >= '0' && character <= '9') {
			continue
		}
		return false
	}
	return true
}
