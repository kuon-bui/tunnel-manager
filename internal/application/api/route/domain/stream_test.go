package domainroute

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"tunnelmanager/internal/model"
	domainrequest "tunnelmanager/internal/pkg/request/domain"
	domainservice "tunnelmanager/internal/services/domain"

	"github.com/gin-gonic/gin"
)

type fakeDomainService struct {
	domainservice.DomainService
	mu          sync.Mutex
	domains     []*model.Domain
	nextCursor  string
	listErr     error
	lastRequest domainrequest.ListDomainRequest
	updates     chan struct{}
	cancelled   chan struct{}
	cancelOnce  sync.Once
}

func (f *fakeDomainService) ListDomains(_ context.Context, req domainrequest.ListDomainRequest) ([]*model.Domain, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.lastRequest = req
	return f.domains, f.nextCursor, f.listErr
}

func (f *fakeDomainService) Subscribe() (<-chan struct{}, func()) {
	return f.updates, func() { f.cancelOnce.Do(func() { close(f.cancelled) }) }
}

func (f *fakeDomainService) setList(domains []*model.Domain, err error) {
	f.mu.Lock()
	f.domains = domains
	f.listErr = err
	f.mu.Unlock()
}

func (f *fakeDomainService) request() domainrequest.ListDomainRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastRequest
}

type sseEvent struct {
	name    string
	data    string
	comment string
}

func readSSEEvent(t *testing.T, reader *bufio.Reader) sseEvent {
	t.Helper()
	var event sseEvent
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("read event: %v", err)
		}
		line = strings.TrimSuffix(strings.TrimSuffix(line, "\n"), "\r")
		if line == "" {
			return event
		}
		switch {
		case strings.HasPrefix(line, "event:"):
			event.name = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			event.data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		case strings.HasPrefix(line, ": "):
			event.comment = strings.TrimPrefix(line, ": ")
		}
	}
}

func openDomainStream(t *testing.T, service *fakeDomainService, rawQuery string) (*http.Response, *bufio.Reader, context.CancelFunc) {
	t.Helper()
	h := &DomainHandler{domainService: service}
	r := gin.New()
	r.GET("/stream", h.streamDomains)
	server := httptest.NewServer(r)
	t.Cleanup(server.Close)
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, server.URL+"/stream"+rawQuery, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp, bufio.NewReader(resp.Body), cancel
}

func TestStreamDomainsSendsInitialFilteredSnapshot(t *testing.T) {
	gin.SetMode(gin.TestMode)
	service := &fakeDomainService{
		domains:   []*model.Domain{{ID: "domain-1", Hostname: "api.example.com"}},
		updates:   make(chan struct{}, 1),
		cancelled: make(chan struct{}),
	}
	resp, reader, cancel := openDomainStream(t, service, "?hostname=api&pageSize=20")
	defer cancel()
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/event-stream") {
		t.Fatalf("content type = %q", got)
	}
	if resp.Header.Get("Cache-Control") != "no-cache" || resp.Header.Get("X-Accel-Buffering") != "no" {
		t.Fatalf("stream headers = %#v", resp.Header)
	}
	event := readSSEEvent(t, reader)
	if event.name != "domains" || !strings.Contains(event.data, `"id":"domain-1"`) {
		t.Fatalf("event = %#v", event)
	}
	req := service.request()
	if req.Hostname != "api" || req.PageSize != 20 {
		t.Fatalf("request = %#v", req)
	}
}

func TestStreamDomainsSendsSnapshotAfterNotification(t *testing.T) {
	service := &fakeDomainService{
		domains:   []*model.Domain{{ID: "domain-1"}},
		updates:   make(chan struct{}, 1),
		cancelled: make(chan struct{}),
	}
	_, reader, cancel := openDomainStream(t, service, "")
	defer cancel()
	_ = readSSEEvent(t, reader)
	service.setList([]*model.Domain{{ID: "domain-2"}}, nil)
	service.updates <- struct{}{}
	event := readSSEEvent(t, reader)
	if event.name != "domains" || !strings.Contains(event.data, `"id":"domain-2"`) {
		t.Fatalf("event = %#v", event)
	}
}

func TestStreamDomainsSendsGenericErrorAndCloses(t *testing.T) {
	service := &fakeDomainService{
		domains:   []*model.Domain{{ID: "domain-1"}},
		updates:   make(chan struct{}, 1),
		cancelled: make(chan struct{}),
	}
	_, reader, cancel := openDomainStream(t, service, "")
	defer cancel()
	_ = readSSEEvent(t, reader)
	service.setList(nil, errors.New("database unavailable"))
	service.updates <- struct{}{}
	event := readSSEEvent(t, reader)
	if event.name != "error" || event.data != `{"message":"stream unavailable"}` {
		t.Fatalf("event = %#v", event)
	}
	if _, err := reader.ReadByte(); !errors.Is(err, io.EOF) {
		t.Fatalf("stream remained open: %v", err)
	}
}

func TestStreamDomainsSendsHeartbeat(t *testing.T) {
	oldInterval := domainStreamHeartbeatInterval
	domainStreamHeartbeatInterval = 10 * time.Millisecond
	t.Cleanup(func() { domainStreamHeartbeatInterval = oldInterval })
	service := &fakeDomainService{updates: make(chan struct{}, 1), cancelled: make(chan struct{})}
	_, reader, cancel := openDomainStream(t, service, "")
	defer cancel()
	_ = readSSEEvent(t, reader)
	event := readSSEEvent(t, reader)
	if event.comment != "heartbeat" {
		t.Fatalf("event = %#v", event)
	}
}

func TestStreamDomainsCancelsSubscriptionOnDisconnect(t *testing.T) {
	service := &fakeDomainService{updates: make(chan struct{}, 1), cancelled: make(chan struct{})}
	_, reader, cancel := openDomainStream(t, service, "")
	_ = readSSEEvent(t, reader)
	cancel()
	select {
	case <-service.cancelled:
	case <-time.After(time.Second):
		t.Fatal("subscription not cancelled")
	}
}

func TestStreamDomainsRejectsInvalidQueryBeforeStreaming(t *testing.T) {
	service := &fakeDomainService{updates: make(chan struct{}, 1), cancelled: make(chan struct{})}
	h := &DomainHandler{domainService: service}
	r := gin.New()
	r.GET("/stream", h.streamDomains)
	server := httptest.NewServer(r)
	defer server.Close()
	resp, err := http.Get(server.URL + "/stream?pageSize=bad")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("response = %d %#v", resp.StatusCode, resp.Header)
	}
}

func TestStreamDomainsReturnsInitialListErrorBeforeStreaming(t *testing.T) {
	service := &fakeDomainService{
		listErr:   errors.New("database unavailable"),
		updates:   make(chan struct{}, 1),
		cancelled: make(chan struct{}),
	}
	h := &DomainHandler{domainService: service}
	r := gin.New()
	r.GET("/stream", h.streamDomains)
	server := httptest.NewServer(r)
	defer server.Close()
	resp, err := http.Get(server.URL + "/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusInternalServerError || strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("response = %d %#v", resp.StatusCode, resp.Header)
	}
}
