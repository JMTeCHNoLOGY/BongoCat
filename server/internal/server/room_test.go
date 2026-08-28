package server

import (
	"encoding/json"
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

func readClientMessage(t *testing.T, client *Client, messageType string) protocol.Message {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()

	for {
		select {
		case data := <-client.send:
			var message protocol.Message
			if err := json.Unmarshal(data, &message); err != nil {
				t.Fatal(err)
			}
			if message.Type == messageType {
				return message
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %q", messageType)
		}
	}
}

func TestHubRoomLimitsNamesResumeAndDestroy(t *testing.T) {
	hub := NewHub(testConfig())
	room, _, err := hub.CreateRoom("First Room")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := hub.CreateRoom("Second Room"); err != nil {
		t.Fatal(err)
	}
	if _, code, err := hub.CreateRoom("Third Room"); err == nil || code != protocol.ErrorRoomLimit {
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

func TestHubListsNamedRoomsInStableOrder(t *testing.T) {
	value := testConfig()
	value.MaxRooms = 3
	value.MaxPlayersPerRoom = 1
	hub := NewHub(value)
	second, _, err := hub.CreateRoom("Beta Room")
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := hub.CreateRoom("Alpha Room")
	if err != nil {
		t.Fatal(err)
	}

	member := testClient(hub)
	if result := second.Join(member, protocol.JoinProfile{Name: "Alice", SkinID: "skin", Mode: "standard"}); result.err != nil {
		t.Fatal(result.err)
	}

	if _, code, err := hub.CreateRoom(" beta room "); err == nil || code != protocol.ErrorRoomNameTaken {
		t.Fatalf("expected duplicate room name, got code=%q err=%v", code, err)
	}

	summaries := hub.ListRooms()
	if len(summaries) != 2 {
		t.Fatalf("expected two rooms, got %d", len(summaries))
	}
	if summaries[0].RoomName != "Alpha Room" || summaries[1].RoomName != "Beta Room" {
		t.Fatalf("rooms were not sorted by name: %+v", summaries)
	}
	if summaries[1].PlayerCount != 1 || summaries[1].MaxPlayers != 1 {
		t.Fatalf("unexpected populated room summary: %+v", summaries[1])
	}

	second.Leave(member, true)
	if first.StopIfEmpty() != true {
		t.Fatal("empty room did not stop")
	}
}

func TestRoomRelaysLatencyAndClearsItDuringReconnect(t *testing.T) {
	hub := NewHub(testConfig())
	room, _, err := hub.CreateRoom("Latency Room")
	if err != nil {
		t.Fatal(err)
	}

	first := testClient(hub)
	firstJoin := room.Join(first, protocol.JoinProfile{Name: "Alice", SkinID: "skin", Mode: "standard"})
	if firstJoin.err != nil {
		t.Fatal(firstJoin.err)
	}
	second := testClient(hub)
	secondJoin := room.Join(second, protocol.JoinProfile{Name: "Bob", SkinID: "skin", Mode: "standard"})
	if secondJoin.err != nil {
		t.Fatal(secondJoin.err)
	}
	readClientMessage(t, first, protocol.TypeMemberJoined)

	room.UpdateLatency(second, 42)
	for _, client := range []*Client{first, second} {
		message := readClientMessage(t, client, protocol.TypeMemberLatency)
		var payload protocol.MemberLatencyPayload
		if err := json.Unmarshal(message.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.PlayerID != secondJoin.joined.Self.PlayerID || payload.LatencyMS == nil || *payload.LatencyMS != 42 {
			t.Fatalf("unexpected latency payload: %+v", payload)
		}
	}

	room.Leave(second, false)
	readClientMessage(t, first, protocol.TypeMemberUpdated)
	cleared := readClientMessage(t, first, protocol.TypeMemberLatency)
	var clearedPayload protocol.MemberLatencyPayload
	if err := json.Unmarshal(cleared.Payload, &clearedPayload); err != nil {
		t.Fatal(err)
	}
	if clearedPayload.PlayerID != secondJoin.joined.Self.PlayerID || clearedPayload.LatencyMS != nil {
		t.Fatalf("unexpected cleared latency payload: %+v", clearedPayload)
	}

	resumed := testClient(hub)
	resume := room.Resume(resumed, secondJoin.joined.Self.PlayerID, secondJoin.joined.ResumeToken)
	if resume.err != nil {
		t.Fatal(resume.err)
	}
	readClientMessage(t, first, protocol.TypeMemberUpdated)

	room.UpdateLatency(second, 99)
	if room.StopIfEmpty() {
		t.Fatal("room unexpectedly stopped")
	}
	select {
	case data := <-first.send:
		t.Fatalf("stale connection produced a broadcast: %s", data)
	default:
	}

	room.UpdateLatency(resumed, 7)
	for _, client := range []*Client{first, resumed} {
		message := readClientMessage(t, client, protocol.TypeMemberLatency)
		var payload protocol.MemberLatencyPayload
		if err := json.Unmarshal(message.Payload, &payload); err != nil {
			t.Fatal(err)
		}
		if payload.LatencyMS == nil || *payload.LatencyMS != 7 {
			t.Fatalf("unexpected resumed latency payload: %+v", payload)
		}
	}

	room.Leave(first, true)
	room.Leave(resumed, true)
}
