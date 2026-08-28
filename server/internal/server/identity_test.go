package server

import (
	"strings"
	"testing"
)

func TestRoomCodeUsesCrockfordAlphabet(t *testing.T) {
	code, err := randomRoomCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 8 || strings.ContainsAny(code, "ILOU") {
		t.Fatalf("invalid room code %q", code)
	}
}

func TestNormalizeName(t *testing.T) {
	name, err := normalizeName("  Cafe\u0301  ")
	if err != nil || name != "Café" {
		t.Fatalf("expected trimmed NFC name, got %q: %v", name, err)
	}
	if _, err := normalizeName("bad\nname"); err == nil {
		t.Fatal("expected control characters to be rejected")
	}
	if _, err := normalizeName(strings.Repeat("猫", 25)); err == nil {
		t.Fatal("expected long names to be rejected")
	}
}

func TestNormalizeRoomName(t *testing.T) {
	name, err := normalizeRoomName("  Cafe\u0301  ")
	if err != nil || name != "Café" {
		t.Fatalf("expected trimmed NFC room name, got %q: %v", name, err)
	}
	if name, err := normalizeRoomName("   "); err != nil || name != "" {
		t.Fatalf("expected blank room name to be allowed for generation, got %q: %v", name, err)
	}
	if _, err := normalizeRoomName("bad\nname"); err == nil {
		t.Fatal("expected control characters to be rejected")
	}
	if _, err := normalizeRoomName(strings.Repeat("猫", 25)); err == nil {
		t.Fatal("expected long room names to be rejected")
	}
}
