package domainroute

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"tunnelmanager/internal/model"

	"github.com/gin-gonic/gin"
)

func openDomainDetailStream(t *testing.T, service *fakeDomainService, id string) (*http.Response, *bufio.Reader, context.CancelFunc) {
	t.Helper()
	h := &DomainHandler{domainService: service}
	r := gin.New()
	r.GET("/:id/stream", h.streamDomainDetail)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/"+id+"/stream", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp, bufio.NewReader(resp.Body), cancel
}

func newDetailStreamService() *fakeDomainService {
	return &fakeDomainService{
		logUpdates:   make(chan struct{}, 1),
		logCancelled: make(chan struct{}),
	}
}

func TestStreamDomainDetailSendsInitialLogsAndMetrics(t *testing.T) {
	service := newDetailStreamService()
	service.logLines = []string{"started"}
	service.metricText = "# HELP tunnel_up\n"
	resp, reader, cancel := openDomainDetailStream(t, service, "domain-1")
	defer cancel()
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content type = %q", got)
	}
	if resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("headers = %#v", resp.Header)
	}
	logs := readSSEEvent(t, reader)
	metrics := readSSEEvent(t, reader)
	if logs.name != "logs" || logs.data != `{"items":["started"]}` {
		t.Fatalf("logs = %#v", logs)
	}
	if metrics.name != "metrics" || metrics.data != `{"text":"# HELP tunnel_up\n"}` {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestStreamDomainDetailReturnsMissingDomainBeforeStreaming(t *testing.T) {
	service := newDetailStreamService()
	service.logErr = model.ErrNotFound
	h := &DomainHandler{domainService: service}
	r := gin.New()
	r.GET("/:id/stream", h.streamDomainDetail)
	server := httptest.NewServer(r)
	defer server.Close()

	resp, err := http.Get(server.URL + "/missing/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("response = %d %#v", resp.StatusCode, resp.Header)
	}
}

func TestStreamDomainDetailRefreshesLogsAfterNotification(t *testing.T) {
	service := newDetailStreamService()
	_, reader, cancel := openDomainDetailStream(t, service, "domain-1")
	defer cancel()
	_ = readSSEEvent(t, reader)
	_ = readSSEEvent(t, reader)
	service.mu.Lock()
	service.logLines = []string{"connected"}
	service.mu.Unlock()
	service.logUpdates <- struct{}{}
	event := readSSEEvent(t, reader)
	if event.name != "logs" || event.data != `{"items":["connected"]}` {
		t.Fatalf("event = %#v", event)
	}
}

func TestStreamDomainDetailKeepsStreamingAfterMetricError(t *testing.T) {
	service := newDetailStreamService()
	service.metricErr = context.DeadlineExceeded
	_, reader, cancel := openDomainDetailStream(t, service, "domain-1")
	defer cancel()
	_ = readSSEEvent(t, reader)
	metricError := readSSEEvent(t, reader)
	if metricError.name != "metrics-error" || metricError.data != `{"message":"metrics unavailable"}` {
		t.Fatalf("metric error = %#v", metricError)
	}
	service.mu.Lock()
	service.logLines = []string{"still running"}
	service.mu.Unlock()
	service.logUpdates <- struct{}{}
	logs := readSSEEvent(t, reader)
	if logs.name != "logs" || logs.data != `{"items":["still running"]}` {
		t.Fatalf("logs = %#v", logs)
	}
}

func TestStreamDomainDetailRefreshesMetrics(t *testing.T) {
	oldInterval := domainDetailMetricsInterval
	domainDetailMetricsInterval = 10 * time.Millisecond
	t.Cleanup(func() { domainDetailMetricsInterval = oldInterval })
	service := newDetailStreamService()
	service.metricText = "first"
	_, reader, cancel := openDomainDetailStream(t, service, "domain-1")
	defer cancel()
	_ = readSSEEvent(t, reader)
	_ = readSSEEvent(t, reader)
	service.mu.Lock()
	service.metricText = "second"
	service.mu.Unlock()
	metrics := readSSEEvent(t, reader)
	if metrics.name != "metrics" || metrics.data != `{"text":"second"}` {
		t.Fatalf("metrics = %#v", metrics)
	}
}

func TestStreamDomainDetailSendsHeartbeat(t *testing.T) {
	oldInterval := domainDetailHeartbeatInterval
	domainDetailHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { domainDetailHeartbeatInterval = oldInterval })
	service := newDetailStreamService()
	_, reader, cancel := openDomainDetailStream(t, service, "domain-1")
	defer cancel()
	_ = readSSEEvent(t, reader)
	_ = readSSEEvent(t, reader)
	event := readSSEEvent(t, reader)
	if event.comment != "heartbeat" {
		t.Fatalf("event = %#v", event)
	}
}

func TestStreamDomainDetailCancelsLogSubscriptionOnDisconnect(t *testing.T) {
	service := newDetailStreamService()
	_, reader, cancel := openDomainDetailStream(t, service, "domain-1")
	_ = readSSEEvent(t, reader)
	_ = readSSEEvent(t, reader)
	cancel()
	select {
	case <-service.logCancelled:
	case <-time.After(time.Second):
		t.Fatal("log subscription not cancelled")
	}
}

func TestStreamDomainDetailClosesAfterLogSnapshotError(t *testing.T) {
	service := newDetailStreamService()
	_, reader, cancel := openDomainDetailStream(t, service, "domain-1")
	defer cancel()
	_ = readSSEEvent(t, reader)
	_ = readSSEEvent(t, reader)
	service.mu.Lock()
	service.logErr = context.Canceled
	service.mu.Unlock()
	service.logUpdates <- struct{}{}
	event := readSSEEvent(t, reader)
	if event.name != "error" || event.data != `{"message":"stream unavailable"}` {
		t.Fatalf("event = %#v", event)
	}
	if _, err := reader.ReadByte(); err != io.EOF {
		t.Fatalf("stream remained open: %v", err)
	}
}
