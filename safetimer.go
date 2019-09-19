package main

import (
	"sync"
	"time"
)

// SafeTime add thread-safe for time.Timer
type SafeTimer struct {
	*time.Timer
	mu       sync.Mutex
	duration time.Duration
}

func NewSafeTimer(d time.Duration) *SafeTimer {
	return &SafeTimer{
		Timer:    time.NewTimer(d),
		duration: d,
	}
}

// Reset is thread-safe now, accept one or none argument
func (t *SafeTimer) Reset(ds ...time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(ds) > 0 {
		if len(ds) != 1 {
			panic("SafeTimer.Reset only accept at most one argument")
		}
		t.duration = ds[0]
	}
	return t.Timer.Reset(t.duration)
}

func (t *SafeTimer) Stop() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Timer.Stop()
}
