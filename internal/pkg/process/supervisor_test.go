package process

import (
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"testing"
	"time"

	"tunnelmanager/internal/pkg/config"
	"tunnelmanager/internal/pkg/constant"
)

// fakeCloudflaredEnv makes the test binary act as a fake cloudflared process
// when the supervisor launches it.
const fakeCloudflaredEnv = "TM_FAKE_CLOUDFLARED"

func TestMain(m *testing.M) {
	switch os.Getenv(fakeCloudflaredEnv) {
	case "":
		os.Exit(m.Run())
	case "run":
		time.Sleep(time.Hour)
	case "ignore-term":
		signal.Ignore(syscall.SIGTERM)
		time.Sleep(time.Hour)
	case "exit":
		os.Exit(1)
	}
	os.Exit(0)
}

type eventRecorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *eventRecorder) record(ev Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, ev)
}

func (r *eventRecorder) last() Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.events) == 0 {
		return Event{}
	}
	return r.events[len(r.events)-1]
}

func newTestSupervisor(t *testing.T, mode string) (*Supervisor, *eventRecorder) {
	t.Helper()
	t.Setenv(fakeCloudflaredEnv, mode)
	sup := NewSupervisor(config.Config{CloudflaredBinary: os.Args[0], CloudflaredProtocol: "quic"}).(*Supervisor)
	recorder := &eventRecorder{}
	sup.SetEventHandler(recorder.record)
	t.Cleanup(func() { _ = sup.StopAndWait("domain-1", 0) })
	return sup, recorder
}

func TestStopAndWaitReturnsAfterProcessExits(t *testing.T) {
	sup, recorder := newTestSupervisor(t, "run")
	if err := sup.Start("domain-1", "token", 20500, io.Discard); err != nil {
		t.Fatal(err)
	}

	if err := sup.StopAndWait("domain-1", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if sup.IsRunning("domain-1") {
		t.Fatal("process still registered after StopAndWait")
	}
	if got := recorder.last().Status; got != constant.StatusStopped {
		t.Fatalf("last event status = %q, want stopped", got)
	}
}

func TestStopAndWaitKillsProcessIgnoringSigterm(t *testing.T) {
	sup, recorder := newTestSupervisor(t, "ignore-term")
	if err := sup.Start("domain-1", "token", 20500, io.Discard); err != nil {
		t.Fatal(err)
	}

	if err := sup.StopAndWait("domain-1", 200*time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if sup.IsRunning("domain-1") {
		t.Fatal("process still registered after StopAndWait")
	}
	if got := recorder.last().Status; got != constant.StatusStopped {
		t.Fatalf("last event status = %q, want stopped", got)
	}
}

func TestStopAndWaitInterruptsRestartBackoff(t *testing.T) {
	sup, recorder := newTestSupervisor(t, "exit")
	if err := sup.Start("domain-1", "token", 20500, io.Discard); err != nil {
		t.Fatal(err)
	}
	// Let the process crash so the supervisor enters its 1s restart backoff.
	time.Sleep(300 * time.Millisecond)

	started := time.Now()
	if err := sup.StopAndWait("domain-1", 5*time.Second); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed > 900*time.Millisecond {
		t.Fatalf("StopAndWait took %s, backoff was not interrupted", elapsed)
	}
	if sup.IsRunning("domain-1") {
		t.Fatal("process restarted after stop was requested")
	}
	if got := recorder.last().Status; got != constant.StatusStopped {
		t.Fatalf("last event status = %q, want stopped", got)
	}
}

func TestStopAndWaitWithoutProcessIsNoop(t *testing.T) {
	sup, _ := newTestSupervisor(t, "run")
	if err := sup.StopAndWait("domain-1", time.Second); err != nil {
		t.Fatal(err)
	}
}
