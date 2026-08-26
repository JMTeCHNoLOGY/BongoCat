package config

import (
	"testing"
	"time"
)

func TestParseDefaults(t *testing.T) {
	value, err := Parse(nil)
	if err != nil {
		t.Fatal(err)
	}
	if value.Listen != ":8080" || value.MaxRooms != 2 || value.MaxPlayersPerRoom != 8 {
		t.Fatalf("unexpected defaults: %+v", value)
	}
	if value.StreamMode != "raw" || value.MaxEventsPerSecond != 512 || value.ContinuousHz != 20 {
		t.Fatalf("unexpected policy defaults: %+v", value)
	}
	if value.ResumeGrace != 15*time.Second || value.MaxMessageBytes != 16384 {
		t.Fatalf("unexpected transport defaults: %+v", value)
	}
}

func TestParseRejectsInvalidLimits(t *testing.T) {
	if _, err := Parse([]string{"--max-rooms=0"}); err == nil {
		t.Fatal("expected invalid max-rooms to fail")
	}
	if _, err := Parse([]string{"--stream-mode=unknown"}); err == nil {
		t.Fatal("expected invalid stream mode to fail")
	}
}
