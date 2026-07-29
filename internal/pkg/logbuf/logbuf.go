package logbuf

import (
	"bufio"
	"os"
	"strings"
	"sync"
)

// Buffer is an io.Writer that keeps the last `capacity` newline-delimited
// lines in memory while also appending every byte written to a file on disk.
type Buffer struct {
	mu          sync.Mutex
	capacity    int
	lines       []string
	partial     string
	file        *os.File
	writer      *bufio.Writer
	subscribers map[chan struct{}]struct{}
}

func NewBuffer(filePath string, capacity int) (*Buffer, error) {
	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Buffer{
		capacity:    capacity,
		file:        f,
		writer:      bufio.NewWriter(f),
		subscribers: make(map[chan struct{}]struct{}),
	}, nil
}

func (b *Buffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if _, err := b.writer.Write(p); err != nil {
		return 0, err
	}
	if err := b.writer.Flush(); err != nil {
		return 0, err
	}

	completed := false
	b.partial += string(p)
	for {
		idx := strings.IndexByte(b.partial, '\n')
		if idx < 0 {
			break
		}
		completed = true
		line := b.partial[:idx]
		b.partial = b.partial[idx+1:]
		b.lines = append(b.lines, line)
		if len(b.lines) > b.capacity {
			b.lines = b.lines[len(b.lines)-b.capacity:]
		}
	}
	if completed {
		for subscriber := range b.subscribers {
			select {
			case subscriber <- struct{}{}:
			default:
			}
		}
	}
	return len(p), nil
}

func (b *Buffer) SnapshotAndSubscribe() ([]string, <-chan struct{}, func()) {
	b.mu.Lock()
	updates := make(chan struct{}, 1)
	b.subscribers[updates] = struct{}{}
	lines := make([]string, len(b.lines))
	copy(lines, b.lines)
	b.mu.Unlock()

	var once sync.Once
	return lines, updates, func() {
		once.Do(func() {
			b.mu.Lock()
			delete(b.subscribers, updates)
			b.mu.Unlock()
		})
	}
}

func (b *Buffer) Lines() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.lines))
	copy(out, b.lines)
	return out
}

func (b *Buffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.writer.Flush(); err != nil {
		return err
	}
	return b.file.Close()
}
