package server

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"bongocat-server/internal/protocol"
	"github.com/coder/websocket"
)

func TestWebSocketFiveClientsAndInputRelay(t *testing.T) {
	value := testConfig()
	value.MaxPlayersPerRoom = 8
	app := New(value)
	httpServer := httptest.NewServer(app.Handler())
	defer httpServer.Close()
	endpoint := "ws" + strings.TrimPrefix(httpServer.URL, "http") + "/v1/ws"
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	clients := make([]*websocket.Conn, 0, 5)
	defer func() {
		for _, client := range clients {
			_ = client.CloseNow()
		}
	}()

	first := dialTestClient(t, ctx, endpoint)
	clients = append(clients, first)
	writeTestMessage(t, ctx, first, protocol.TypeCreateRoom, map[string]any{"profile": map[string]any{"name": "Player1", "skinId": "skin", "mode": "standard"}})
	joined := readUntil(t, ctx, first, protocol.TypeRoomJoined)
	var room protocol.RoomJoined
	if err := json.Unmarshal(joined.Payload, &room); err != nil {
		t.Fatal(err)
	}

	for index := 2; index <= 5; index++ {
		client := dialTestClient(t, ctx, endpoint)
		clients = append(clients, client)
		writeTestMessage(t, ctx, client, protocol.TypeJoinRoom, map[string]any{
			"roomCode": room.RoomCode,
			"profile":  map[string]any{"name": "Player" + string(rune('0'+index)), "skinId": "skin", "mode": "standard"},
		})
		readUntil(t, ctx, client, protocol.TypeRoomJoined)
		readUntil(t, ctx, first, protocol.TypeMemberJoined)
	}

	writeTestMessage(t, ctx, clients[1], protocol.TypeInput, map[string]any{
		"event": map[string]any{"sequence": 1, "clientTimeMs": 1, "kind": "KeyboardPress", "value": "A"},
	})
	relayed := readUntil(t, ctx, first, protocol.TypeInput)
	var payload protocol.InputEventPayload
	if err := json.Unmarshal(relayed.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.PlayerID == "" || payload.Event.Kind != "KeyboardPress" || string(payload.Event.Value) != `"A"` {
		t.Fatalf("unexpected relayed input: %+v", payload)
	}

	latencyMessage := readUntil(t, ctx, first, protocol.TypeMemberLatency)
	var latency protocol.MemberLatencyPayload
	if err := json.Unmarshal(latencyMessage.Payload, &latency); err != nil {
		t.Fatal(err)
	}
	if latency.PlayerID != room.Self.PlayerID || latency.LatencyMS == nil || *latency.LatencyMS < 0 {
		t.Fatalf("unexpected relayed latency: %+v", latency)
	}
}

func dialTestClient(t *testing.T, ctx context.Context, endpoint string) *websocket.Conn {
	t.Helper()
	client, _, err := websocket.Dial(ctx, endpoint, nil)
	if err != nil {
		t.Fatal(err)
	}
	readUntil(t, ctx, client, protocol.TypePolicy)
	return client
}

func writeTestMessage(t *testing.T, ctx context.Context, client *websocket.Conn, messageType string, payload any) {
	t.Helper()
	data, err := protocol.NewMessage(messageType, "test", payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func readUntil(t *testing.T, ctx context.Context, client *websocket.Conn, messageType string) protocol.Message {
	t.Helper()
	for {
		typeID, data, err := client.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if typeID != websocket.MessageText {
			continue
		}
		var message protocol.Message
		if err := json.Unmarshal(data, &message); err != nil {
			t.Fatal(err)
		}
		if message.Type == messageType {
			return message
		}
	}
}
