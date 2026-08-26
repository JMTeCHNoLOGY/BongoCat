package server

import (
	"testing"
	"time"

	"bongocat-server/internal/config"
	"bongocat-server/internal/protocol"
)

func testConfig() config.Config {
	return config.Config{
		Listen: ":0", MaxRooms: 2, MaxPlayersPerRoom: 2, StreamMode: protocol.StreamModeRaw,
		MaxEventsPerSecond: 512, ContinuousHz: 20, MaxMessageBytes: 16384,
		ResumeGrace: time.Second, SnapshotIntervalMillis: 1000,
	}
}

func testClient(hub *Hub) *Client {
	return &Client{hub: hub, send: make(chan []byte, 256), done: make(chan struct{}), limiter: newEventLimiter(512)}
}

func TestHubRoomLimitsNamesResumeAndDestroy(t *testing.T) {
	hub := NewHub(testConfig())
	room, _, err := hub.CreateRoom()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hub.CreateRoom(); err != nil {
		t.Fatal(err)
	}
	if _, code, err := hub.CreateRoom(); err == nil || code != protocol.ErrorRoomLimit {
		t.Fatalf("expected room limit, got code=%q err=%v", code, err)
	}

	first := testClient(hub)
	joined := room.Join(first, protocol.JoinProfile{Name: "Alice", SkinID: "builtin:standard:v1", Mode: "standard"})
	if joined.err != nil {
		t.Fatal(joined.err)
	}
	duplicate := room.Join(testClient(hub), protocol.JoinProfile{Name: " alice ", SkinID: "skin", Mode: "standard"})
	if duplicate.code != protocol.ErrorNameTaken {
		t.Fatalf("expected NAME_TAKEN, got %q", duplicate.code)
	}
	second := testClient(hub)
	if result := room.Join(second, protocol.JoinProfile{Name: "Bob", SkinID: "skin", Mode: "keyboard"}); result.err != nil {
		t.Fatal(result.err)
	}
	if result := room.Join(testClient(hub), protocol.JoinProfile{Name: "Carol", SkinID: "skin", Mode: "standard"}); result.code != protocol.ErrorRoomFull {
		t.Fatalf("expected ROOM_FULL, got %q", result.code)
	}

	firstToken := joined.joined.ResumeToken
	firstID := joined.joined.Self.PlayerID
	room.Leave(first, false)
	resumedClient := testClient(hub)
	if result := room.Resume(resumedClient, firstID, firstToken); result.err != nil || !result.joined.Self.Online {
		t.Fatalf("resume failed: %+v", result)
	}
	room.Leave(resumedClient, true)
	room.Leave(second, true)

	deadline := time.Now().Add(time.Second)
	for hub.RoomCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if hub.RoomCount() != 1 {
		t.Fatalf("empty room was not destroyed, rooms=%d", hub.RoomCount())
	}
}
