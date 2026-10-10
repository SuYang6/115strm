package logbuffer

import (
	"fmt"
	"os"
	"sync"
	"time"
)

type Entry struct {
	Time  time.Time `json:"time"`
	Level string    `json:"level"`
	Text  string    `json:"text"`
}

type Buffer struct {
	mu      sync.RWMutex
	entries []Entry
	max     int
	file    *os.File
	subs    map[chan Entry]struct{}
}

func New(path string, max int) (*Buffer, error) {
	if max <= 0 {
		max = 500
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return nil, err
	}
	return &Buffer{max: max, file: f, subs: map[chan Entry]struct{}{}}, nil
}

func (b *Buffer) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.file != nil {
		return b.file.Close()
	}
	return nil
}

func (b *Buffer) Info(format string, args ...any)  { b.add("info", format, args...) }
func (b *Buffer) Warn(format string, args ...any)  { b.add("warn", format, args...) }
func (b *Buffer) Error(format string, args ...any) { b.add("error", format, args...) }

func (b *Buffer) add(level, format string, args ...any) {
	e := Entry{Time: time.Now(), Level: level, Text: fmt.Sprintf(format, args...)}
	line := fmt.Sprintf("%s [%s] %s\n", e.Time.Format(time.RFC3339), e.Level, e.Text)

	b.mu.Lock()
	b.entries = append(b.entries, e)
	if len(b.entries) > b.max {
		b.entries = b.entries[len(b.entries)-b.max:]
	}
	if b.file != nil {
		_, _ = b.file.WriteString(line)
	}
	_, _ = os.Stdout.WriteString(line)
	for ch := range b.subs {
		select {
		case ch <- e:
		default:
		}
	}
	b.mu.Unlock()
}

func (b *Buffer) List(limit int) []Entry {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if limit <= 0 || limit > len(b.entries) {
		limit = len(b.entries)
	}
	start := len(b.entries) - limit
	out := make([]Entry, limit)
	copy(out, b.entries[start:])
	return out
}

func (b *Buffer) Subscribe() (<-chan Entry, func()) {
	ch := make(chan Entry, 32)
	b.mu.Lock()
	b.subs[ch] = struct{}{}
	b.mu.Unlock()
	cancel := func() {
		b.mu.Lock()
		if _, ok := b.subs[ch]; ok {
			delete(b.subs, ch)
			close(ch)
		}
		b.mu.Unlock()
	}
	return ch, cancel
}
