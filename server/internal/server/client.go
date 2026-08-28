package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"bongocat-server/internal/protocol"
	"github.com/coder/websocket"
)

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	done     chan struct{}
	limiter  *eventLimiter
	mu       sync.RWMutex
	room     *Room
	playerID string
	closed   sync.Once
}

func NewClient(hub *Hub, conn *websocket.Conn) *Client {
	return &Client{
		hub:     hub,
		conn:    conn,
		send:    make(chan []byte, 256),
		done:    make(chan struct{}),
		limiter: newEventLimiter(hub.policy.MaxEventsPerSec),
	}
}

func (client *Client) Run(ctx context.Context) {
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		client.writeLoop(ctx)
	}()

	client.sendMessage(protocol.TypePolicy, "", client.hub.Policy())
	client.readLoop(ctx)
	client.shutdown()
	<-writerDone
}

func (client *Client) readLoop(ctx context.Context) {
	for {
		messageType, data, err := client.conn.Read(ctx)
		if err != nil {
			return
		}
		if messageType != websocket.MessageText {
			client.sendError("", protocol.ErrorInvalidMessage, "only JSON text messages are accepted")
			continue
		}

		var message protocol.Message
		if err := json.Unmarshal(data, &message); err != nil || message.Version != protocol.Version {
			client.sendError(message.RequestID, protocol.ErrorInvalidMessage, "invalid protocol message")
			continue
		}

		if !client.handleMessage(message) {
			return
		}
	}
}

func (client *Client) writeLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-client.done:
			return
		case data := <-client.send:
			writeContext, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := client.conn.Write(writeContext, websocket.MessageText, data)
			cancel()
			if err != nil {
				return
			}
		case <-ticker.C:
			pingContext, cancel := context.WithTimeout(ctx, 10*time.Second)
			started := time.Now()
			err := client.conn.Ping(pingContext)
			cancel()
			if err != nil {
				return
			}
			if room := client.roomValue(); room != nil {
				room.UpdateLatency(client, time.Since(started).Milliseconds())
			}
		}
	}
}

func (client *Client) handleMessage(message protocol.Message) bool {
	switch message.Type {
	case protocol.TypeCreateRoom:
		if client.joined() {
			client.sendError(message.RequestID, protocol.ErrorAlreadyJoined, "leave the current room first")
			return true
		}
		payload, err := protocol.DecodePayload[protocol.CreateRoomRequest](message)
		if err != nil {
			client.sendError(message.RequestID, protocol.ErrorInvalidMessage, err.Error())
			return true
		}
		room, code, err := client.hub.CreateRoom(payload.RoomName)
		if err != nil {
			client.sendError(message.RequestID, code, err.Error())
			return true
		}
		result := room.Join(client, payload.Profile)
		if result.err != nil {
			room.StopIfEmpty()
			client.sendError(message.RequestID, result.code, result.err.Error())
			return true
		}
		client.sendMessage(protocol.TypeRoomJoined, message.RequestID, result.joined)
	case protocol.TypeListRooms:
		if client.joined() {
			client.sendError(message.RequestID, protocol.ErrorAlreadyJoined, "leave the current room first")
			return true
		}
		if _, err := protocol.DecodePayload[protocol.ListRoomsRequest](message); err != nil {
			client.sendError(message.RequestID, protocol.ErrorInvalidMessage, err.Error())
			return true
		}
		client.sendMessage(protocol.TypeRoomList, message.RequestID, protocol.RoomList{Rooms: client.hub.ListRooms()})
	case protocol.TypeJoinRoom:
		if client.joined() {
			client.sendError(message.RequestID, protocol.ErrorAlreadyJoined, "leave the current room first")
			return true
		}
		payload, err := protocol.DecodePayload[protocol.JoinRoomRequest](message)
		if err != nil {
			client.sendError(message.RequestID, protocol.ErrorInvalidMessage, err.Error())
			return true
		}
		room := client.hub.Room(payload.RoomCode)
		if room == nil {
			client.sendError(message.RequestID, protocol.ErrorRoomNotFound, "room does not exist")
			return true
		}
		result := room.Join(client, payload.Profile)
		if result.err != nil {
			client.sendError(message.RequestID, result.code, result.err.Error())
			return true
		}
		client.sendMessage(protocol.TypeRoomJoined, message.RequestID, result.joined)
	case protocol.TypeResumeRoom:
		if client.joined() {
			client.sendError(message.RequestID, protocol.ErrorAlreadyJoined, "connection already joined")
			return true
		}
		payload, err := protocol.DecodePayload[protocol.ResumeRoomRequest](message)
		if err != nil {
			client.sendError(message.RequestID, protocol.ErrorInvalidMessage, err.Error())
			return true
		}
		room := client.hub.Room(payload.RoomCode)
		if room == nil {
			client.sendError(message.RequestID, protocol.ErrorSessionExpired, "room is no longer available")
			return true
		}
		result := room.Resume(client, payload.PlayerID, payload.ResumeToken)
		if result.err != nil {
			client.sendError(message.RequestID, result.code, result.err.Error())
			return true
		}
		client.sendMessage(protocol.TypeRoomJoined, message.RequestID, result.joined)
	case protocol.TypeLeaveRoom:
		room := client.roomValue()
		if room == nil {
			client.sendError(message.RequestID, protocol.ErrorNotJoined, "connection is not in a room")
			return true
		}
		room.Leave(client, true)
	case protocol.TypeInput:
		room := client.roomValue()
		if room == nil {
			client.sendError(message.RequestID, protocol.ErrorNotJoined, "connection is not in a room")
			return true
		}
		payload, err := protocol.DecodePayload[protocol.InputEventPayload](message)
		if err != nil || !validInputEvent(payload.Event) {
			client.sendError(message.RequestID, protocol.ErrorInvalidMessage, "invalid input event")
			return true
		}
		allowed, violations := client.limiter.allow(time.Now())
		if !allowed {
			if !protocol.IsContinuousEvent(payload.Event.Kind) {
				client.sendError(message.RequestID, protocol.ErrorRateLimited, "input event rate exceeded")
			}
			if violations >= client.hub.policy.MaxEventsPerSec*3 {
				return false
			}
			return true
		}
		room.Input(client, payload.Event)
	case protocol.TypeSnapshot:
		room := client.roomValue()
		if room == nil {
			client.sendError(message.RequestID, protocol.ErrorNotJoined, "connection is not in a room")
			return true
		}
		payload, err := protocol.DecodePayload[protocol.SnapshotPayload](message)
		if err != nil || !validSnapshot(payload.Snapshot) {
			client.sendError(message.RequestID, protocol.ErrorInvalidMessage, "invalid input snapshot")
			return true
		}
		room.Snapshot(client, payload.Snapshot)
	case protocol.TypeProfileUpdate:
		room := client.roomValue()
		if room == nil {
			client.sendError(message.RequestID, protocol.ErrorNotJoined, "connection is not in a room")
			return true
		}
		payload, err := protocol.DecodePayload[protocol.ProfileUpdate](message)
		if err != nil || !protocol.IsModelMode(payload.Mode) || payload.SkinID == "" {
			client.sendError(message.RequestID, protocol.ErrorInvalidMessage, "invalid profile")
			return true
		}
		room.UpdateProfile(client, payload)
	default:
		client.sendError(message.RequestID, protocol.ErrorInvalidMessage, fmt.Sprintf("unsupported message type %q", message.Type))
	}

	return true
}

func validInputEvent(event protocol.InputEvent) bool {
	return event.Sequence > 0 && protocol.IsInputEvent(event.Kind) && len(event.Value) > 0 && len(event.Value) <= 4096
}

func validSnapshot(snapshot protocol.InputSnapshot) bool {
	return snapshot.Sequence > 0 && len(snapshot.PressedKeys) <= 128 && len(snapshot.MouseButtons) <= 16 && len(snapshot.Gamepad) <= 64
}

func (client *Client) sendMessage(messageType, requestID string, payload any) {
	data, err := protocol.NewMessage(messageType, requestID, payload)
	if err != nil || !client.enqueue(data) {
		client.shutdown()
	}
}

func (client *Client) sendError(requestID, code, message string) {
	client.sendMessage(protocol.TypeError, requestID, protocol.ErrorPayload{Code: code, Message: message})
}

func (client *Client) enqueue(data []byte) bool {
	select {
	case <-client.done:
		return false
	default:
	}
	select {
	case <-client.done:
		return false
	case client.send <- data:
		return true
	default:
		return false
	}
}

func (client *Client) shutdown() {
	client.closed.Do(func() {
		if room := client.roomValue(); room != nil {
			room.Leave(client, false)
		}
		close(client.done)
		_ = client.conn.CloseNow()
	})
}

func (client *Client) attach(room *Room, playerID string) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.room = room
	client.playerID = playerID
}

func (client *Client) detach(room *Room) {
	client.mu.Lock()
	defer client.mu.Unlock()
	if client.room == room {
		client.room = nil
		client.playerID = ""
	}
}

func (client *Client) joined() bool {
	return client.roomValue() != nil
}

func (client *Client) roomValue() *Room {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.room
}

func (client *Client) playerIDValue() string {
	client.mu.RLock()
	defer client.mu.RUnlock()
	return client.playerID
}

func logClientError(message string, err error) {
	if err != nil {
		slog.Debug(message, "error", err)
	}
}
