package server

import (
	"sync"
	"time"
)

type eventLimiter struct {
	mu         sync.Mutex
	limit      int
	window     time.Time
	count      int
	violations int
}

func newEventLimiter(limit int) *eventLimiter {
	return &eventLimiter{limit: limit, window: time.Now()}
}

func (limiter *eventLimiter) allow(now time.Time) (bool, int) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()

	if now.Sub(limiter.window) >= time.Second {
		if limiter.count <= limiter.limit {
			limiter.violations = 0
		}
		limiter.window = now
		limiter.count = 0
	}

	limiter.count++
	if limiter.count <= limiter.limit {
		return true, limiter.violations
	}

	limiter.violations++
	return false, limiter.violations
}
