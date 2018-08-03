package main

import (
	"sync"
	"time"
)

// SafeTime add thread-safe for time.Timer
type SafeTimer struct {
	*time.Timer
	mu sync.Mutex
}

func NewSafeTimer(d time.Duration) *SafeTimer {
	return &SafeTimer{
		Timer: time.NewTimer(d),
	}
}

// Reset is thread-safe now
func (t *SafeTimer) Reset(d time.Duration) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Timer.Reset(d)
}
