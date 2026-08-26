package server

import (
	"fmt"
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

func (hub *Hub) CreateRoom() (*Room, string, error) {
	hub.mu.Lock()
	defer hub.mu.Unlock()

	if len(hub.rooms) >= hub.config.MaxRooms {
		return nil, protocol.ErrorRoomLimit, fmt.Errorf("room limit reached")
	}

	for attempts := 0; attempts < 16; attempts++ {
		code, err := randomRoomCode()
		if err != nil {
			return nil, protocol.ErrorInvalidMessage, err
		}
		if _, exists := hub.rooms[code]; exists {
			continue
		}

		room := NewRoom(code, hub.policy, hub.config.ResumeGrace, hub.removeRoom)
		hub.rooms[code] = room
		go room.Run()
		return room, "", nil
	}

	return nil, protocol.ErrorInvalidMessage, fmt.Errorf("could not allocate a room code")
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
