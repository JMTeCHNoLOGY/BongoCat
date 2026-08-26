package server

import (
	"testing"
	"time"
)

func TestEventLimiterResetsAfterWindow(t *testing.T) {
	limiter := newEventLimiter(2)
	now := time.Now()
	if allowed, _ := limiter.allow(now); !allowed {
		t.Fatal("first event should pass")
	}
	if allowed, _ := limiter.allow(now); !allowed {
		t.Fatal("second event should pass")
	}
	if allowed, violations := limiter.allow(now); allowed || violations != 1 {
		t.Fatalf("third event should be limited, got allowed=%v violations=%d", allowed, violations)
	}
	if allowed, _ := limiter.allow(now.Add(time.Second)); !allowed {
		t.Fatal("new window should accept events")
	}
}
