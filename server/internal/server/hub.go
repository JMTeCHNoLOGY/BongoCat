package server

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"bongocat-server/internal/config"
	"bongocat-server/internal/protocol"
)

type Hub struct {
	config config.Config
	policy protocol.RoomPolicy
	mu     sync.RWMutex
	rooms  map[string]*Room
}

func NewHub(config config.Config) *Hub {
	return &Hub{
		config: config,
		policy: config.Policy(),
		rooms:  make(map[string]*Room),
	}
}

func (hub *Hub) Policy() protocol.RoomPolicy {
	return hub.policy
}

func (hub *Hub) CreateRoom(roomName string) (*Room, string, error) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	if len(hub.rooms) >= hub.config.MaxRooms {
		return nil, protocol.ErrorRoomLimit, fmt.Errorf("room limit reached")
	}

	normalizedName, err := normalizeRoomName(roomName)
	if err != nil {
		return nil, protocol.ErrorInvalidRoomName, err
	}
	if normalizedName != "" && hub.roomNameTakenLocked(normalizedName) {
		return nil, protocol.ErrorRoomNameTaken, fmt.Errorf("room name is already in use")
	}

	for attempts := 0; attempts < 16; attempts++ {
		resolvedName := normalizedName
		if resolvedName == "" {
			resolvedName, err = randomRoomName()
			if err != nil {
				return nil, protocol.ErrorInvalidMessage, err
			}
			if hub.roomNameTakenLocked(resolvedName) {
				continue
			}
		}

		code, err := randomRoomCode()
		if err != nil {
			return nil, protocol.ErrorInvalidMessage, err
		}
		if _, exists := hub.rooms[code]; exists {
			continue
		}

		room := NewRoom(code, resolvedName, hub.policy, hub.config.ResumeGrace, hub.removeRoom)
		hub.rooms[code] = room
		go room.Run()
		return room, "", nil
	}

	return nil, protocol.ErrorInvalidMessage, fmt.Errorf("could not allocate a room code")
}

func (hub *Hub) ListRooms() []protocol.RoomSummary {
	hub.mu.RLock()
	rooms := make([]*Room, 0, len(hub.rooms))
	for _, room := range hub.rooms {
		rooms = append(rooms, room)
	}
	hub.mu.RUnlock()

	summaries := make([]protocol.RoomSummary, 0, len(rooms))
	for _, room := range rooms {
		if summary, ok := room.Summary(); ok {
			summaries = append(summaries, summary)
		}
	}
	sort.Slice(summaries, func(left, right int) bool {
		leftName := strings.ToLower(summaries[left].RoomName)
		rightName := strings.ToLower(summaries[right].RoomName)
		if leftName != rightName {
			return leftName < rightName
		}
		return summaries[left].RoomCode < summaries[right].RoomCode
	})
	return summaries
}

func (hub *Hub) Room(code string) *Room {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	return hub.rooms[normalizeRoomCode(code)]
}

func (hub *Hub) removeRoom(code string, room *Room) {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.rooms[code] == room {
		delete(hub.rooms, code)
	}
}

func (hub *Hub) RoomCount() int {
	hub.mu.RLock()
	defer hub.mu.RUnlock()
	return len(hub.rooms)
}

func (hub *Hub) roomNameTakenLocked(roomName string) bool {
	for _, room := range hub.rooms {
		if strings.EqualFold(room.name, roomName) {
			return true
		}
	}
	return false
}
