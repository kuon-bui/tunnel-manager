package logbuf

import (
	"path/filepath"
	"testing"
	"time"
)

func TestSnapshotAndSubscribePublishesCompleteLinesAndCapsSnapshot(t *testing.T) {
	buf, err := NewBuffer(filepath.Join(t.TempDir(), "domain.log"), 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = buf.Close() })

	initial, updates, cancel := buf.SnapshotAndSubscribe()
	defer cancel()
	if len(initial) != 0 {
		t.Fatalf("initial = %#v", initial)
	}

	if _, err := buf.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
		t.Fatal("partial line published")
	default:
	}

	if _, err := buf.Write([]byte(" one\nsecond\nthird\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("complete lines did not publish")
	}
	if got := buf.Lines(); len(got) != 2 || got[0] != "second" || got[1] != "third" {
		t.Fatalf("lines = %#v", got)
	}
}

func TestLogNotificationsCoalesceAndCancellationIsIdempotent(t *testing.T) {
	buf, err := NewBuffer(filepath.Join(t.TempDir(), "domain.log"), 500)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = buf.Close() })

	_, updates, cancel := buf.SnapshotAndSubscribe()
	if _, err := buf.Write([]byte("one\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := buf.Write([]byte("two\n")); err != nil {
		t.Fatal(err)
	}
	<-updates
	select {
	case <-updates:
		t.Fatal("notifications did not coalesce")
	default:
	}

	cancel()
	cancel()
	if _, err := buf.Write([]byte("three\n")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-updates:
		t.Fatal("cancelled subscriber received notification")
	default:
	}
}
