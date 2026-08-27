package server

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"bongocat-server/internal/protocol"
)

type member struct {
	profile     protocol.PlayerProfile
	resumeToken string
	client      *Client
	snapshot    *protocol.InputSnapshot
}

type joinRequest struct {
	client  *Client
	profile protocol.JoinProfile
	result  chan roomResult
}

type resumeRequest struct {
	client      *Client
	playerID    string
	resumeToken string
	result      chan roomResult
}

type leaveRequest struct {
	client   *Client
	explicit bool
}

type expireRequest struct {
	playerID    string
	resumeToken string
}

type inputRequest struct {
	client *Client
	event  protocol.InputEvent
}

type snapshotRequest struct {
	client   *Client
	snapshot protocol.InputSnapshot
}

type profileRequest struct {
	client  *Client
	profile protocol.ProfileUpdate
}

type latencyRequest struct {
	client    *Client
	latencyMS int64
}

type stopIfEmptyRequest struct {
	result chan bool
}

type roomResult struct {
	joined *protocol.RoomJoined
	code   string
	err    error
}

type Room struct {
	code        string
	policy      protocol.RoomPolicy
	resumeGrace time.Duration
	commands    chan any
	done        chan struct{}
	members     map[string]*member
	nextOrder   uint64
	onEmpty     func(string, *Room)
}

func NewRoom(code string, policy protocol.RoomPolicy, resumeGrace time.Duration, onEmpty func(string, *Room)) *Room {
	return &Room{
		code:        code,
		policy:      policy,
		resumeGrace: resumeGrace,
		commands:    make(chan any, 256),
		done:        make(chan struct{}),
		members:     make(map[string]*member),
		onEmpty:     onEmpty,
	}
}

func (room *Room) Run() {
	defer close(room.done)
	for command := range room.commands {
		switch value := command.(type) {
		case joinRequest:
			room.handleJoin(value)
		case resumeRequest:
			room.handleResume(value)
		case leaveRequest:
			if room.handleLeave(value) {
				return
			}
		case expireRequest:
			if room.handleExpire(value) {
				return
			}
		case inputRequest:
			room.handleInput(value)
		case snapshotRequest:
			room.handleSnapshot(value)
		case profileRequest:
			room.handleProfile(value)
		case latencyRequest:
			room.handleLatency(value)
		case stopIfEmptyRequest:
			empty := len(room.members) == 0
			value.result <- empty
			if empty {
				room.onEmpty(room.code, room)
				return
			}
		}
	}
}

func (room *Room) Join(client *Client, profile protocol.JoinProfile) roomResult {
	result := make(chan roomResult, 1)
	if !room.send(joinRequest{client: client, profile: profile, result: result}) {
		return roomResult{code: protocol.ErrorRoomNotFound, err: fmt.Errorf("room is closed")}
	}
	select {
	case response := <-result:
		return response
	case <-room.done:
		return roomResult{code: protocol.ErrorRoomNotFound, err: fmt.Errorf("room is closed")}
	}
}

func (room *Room) Resume(client *Client, playerID, resumeToken string) roomResult {
	result := make(chan roomResult, 1)
	if !room.send(resumeRequest{client: client, playerID: playerID, resumeToken: resumeToken, result: result}) {
		return roomResult{code: protocol.ErrorSessionExpired, err: fmt.Errorf("room is closed")}
	}
	select {
	case response := <-result:
		return response
	case <-room.done:
		return roomResult{code: protocol.ErrorSessionExpired, err: fmt.Errorf("room is closed")}
	}
}

func (room *Room) Leave(client *Client, explicit bool) {
	room.send(leaveRequest{client: client, explicit: explicit})
}

func (room *Room) Input(client *Client, event protocol.InputEvent) {
	room.send(inputRequest{client: client, event: event})
}

func (room *Room) Snapshot(client *Client, snapshot protocol.InputSnapshot) {
	room.send(snapshotRequest{client: client, snapshot: snapshot})
}

func (room *Room) UpdateProfile(client *Client, profile protocol.ProfileUpdate) {
	room.send(profileRequest{client: client, profile: profile})
}

func (room *Room) UpdateLatency(client *Client, latencyMS int64) {
	room.send(latencyRequest{client: client, latencyMS: latencyMS})
}

func (room *Room) StopIfEmpty() bool {
	result := make(chan bool, 1)
	if !room.send(stopIfEmptyRequest{result: result}) {
		return true
	}
	select {
	case stopped := <-result:
		return stopped
	case <-room.done:
		return true
	}
}

func (room *Room) send(command any) bool {
	select {
	case room.commands <- command:
		return true
	case <-room.done:
		return false
	}
}

func (room *Room) handleJoin(request joinRequest) {
	name, err := normalizeName(request.profile.Name)
	if err != nil {
		request.result <- roomResult{code: protocol.ErrorInvalidName, err: err}
		return
	}
	if !protocol.IsModelMode(request.profile.Mode) || strings.TrimSpace(request.profile.SkinID) == "" {
		request.result <- roomResult{code: protocol.ErrorUnsupportedMode, err: fmt.Errorf("valid skin and model mode are required")}
		return
	}
	if len(room.members) >= room.policy.MaxPlayers {
		request.result <- roomResult{code: protocol.ErrorRoomFull, err: fmt.Errorf("room is full")}
		return
	}
	for _, existing := range room.members {
		if strings.EqualFold(existing.profile.Name, name) {
			request.result <- roomResult{code: protocol.ErrorNameTaken, err: fmt.Errorf("name is already in use")}
			return
		}
	}

	playerID, err := randomHex(12)
	if err != nil {
		request.result <- roomResult{code: protocol.ErrorInvalidMessage, err: err}
		return
	}
	resumeToken, err := randomHex(24)
	if err != nil {
		request.result <- roomResult{code: protocol.ErrorInvalidMessage, err: err}
		return
	}

	room.nextOrder++
	profile := protocol.PlayerProfile{
		PlayerID: playerID,
		Name:     name,
		SkinID:   strings.TrimSpace(request.profile.SkinID),
		Mode:     request.profile.Mode,
		Order:    room.nextOrder,
		Online:   true,
	}
	room.members[playerID] = &member{profile: profile, resumeToken: resumeToken, client: request.client}
	request.client.attach(room, playerID)

	joined := room.joinedPayload(profile, resumeToken)
	request.result <- roomResult{joined: &joined}
	room.broadcast(protocol.TypeMemberJoined, protocol.MemberPayload{Player: profile}, request.client)
}

func (room *Room) handleResume(request resumeRequest) {
	current := room.members[request.playerID]
	if current == nil || current.resumeToken != request.resumeToken {
		request.result <- roomResult{code: protocol.ErrorSessionExpired, err: fmt.Errorf("session is not available")}
		return
	}
	if current.client != nil && current.client != request.client {
		request.result <- roomResult{code: protocol.ErrorAlreadyJoined, err: fmt.Errorf("session is already connected")}
		return
	}

	current.client = request.client
	current.profile.Online = true
	request.client.attach(room, request.playerID)
	joined := room.joinedPayload(current.profile, current.resumeToken)
	request.result <- roomResult{joined: &joined}
	room.broadcast(protocol.TypeMemberUpdated, protocol.MemberPayload{Player: current.profile}, request.client)
}

func (room *Room) handleLeave(request leaveRequest) bool {
	playerID := request.client.playerIDValue()
	current := room.members[playerID]
	if current == nil || current.client != request.client {
		return false
	}

	request.client.detach(room)
	if request.explicit || room.resumeGrace == 0 {
		return room.removeMember(playerID)
	}

	current.client = nil
	current.profile.Online = false
	room.broadcast(protocol.TypeMemberUpdated, protocol.MemberPayload{Player: current.profile}, nil)
	room.broadcast(protocol.TypeMemberLatency, protocol.MemberLatencyPayload{PlayerID: playerID, LatencyMS: nil}, nil)
	resumeToken := current.resumeToken
	time.AfterFunc(room.resumeGrace, func() {
		room.send(expireRequest{playerID: playerID, resumeToken: resumeToken})
	})
	return false
}

func (room *Room) handleExpire(request expireRequest) bool {
	current := room.members[request.playerID]
	if current == nil || current.client != nil || current.resumeToken != request.resumeToken {
		return false
	}
	return room.removeMember(request.playerID)
}

func (room *Room) handleInput(request inputRequest) {
	playerID := request.client.playerIDValue()
	if room.members[playerID] == nil {
		return
	}
	room.broadcast(protocol.TypeInput, protocol.InputEventPayload{PlayerID: playerID, Event: request.event}, request.client)
}

func (room *Room) handleSnapshot(request snapshotRequest) {
	playerID := request.client.playerIDValue()
	current := room.members[playerID]
	if current == nil {
		return
	}
	copy := request.snapshot
	current.snapshot = &copy
	room.broadcast(protocol.TypeSnapshot, protocol.SnapshotPayload{PlayerID: playerID, Snapshot: copy}, request.client)
}

func (room *Room) handleProfile(request profileRequest) {
	playerID := request.client.playerIDValue()
	current := room.members[playerID]
	if current == nil || !protocol.IsModelMode(request.profile.Mode) || strings.TrimSpace(request.profile.SkinID) == "" {
		return
	}
	current.profile.SkinID = strings.TrimSpace(request.profile.SkinID)
	current.profile.Mode = request.profile.Mode
	room.broadcast(protocol.TypeMemberUpdated, protocol.MemberPayload{Player: current.profile}, request.client)
}

func (room *Room) handleLatency(request latencyRequest) {
	playerID := request.client.playerIDValue()
	current := room.members[playerID]
	if current == nil || current.client != request.client || !current.profile.Online {
		return
	}

	latencyMS := max(request.latencyMS, 0)
	room.broadcast(protocol.TypeMemberLatency, protocol.MemberLatencyPayload{
		PlayerID:  playerID,
		LatencyMS: &latencyMS,
	}, nil)
}

func (room *Room) removeMember(playerID string) bool {
	if _, exists := room.members[playerID]; !exists {
		return false
	}
	delete(room.members, playerID)
	room.broadcast(protocol.TypeMemberLeft, protocol.MemberLeft{PlayerID: playerID}, nil)
	if len(room.members) == 0 {
		room.onEmpty(room.code, room)
		return true
	}
	return false
}

func (room *Room) joinedPayload(self protocol.PlayerProfile, resumeToken string) protocol.RoomJoined {
	players := make([]protocol.PlayerProfile, 0, len(room.members))
	for _, current := range room.members {
		players = append(players, current.profile)
	}
	sort.Slice(players, func(left, right int) bool { return players[left].Order < players[right].Order })
	return protocol.RoomJoined{
		RoomCode:    room.code,
		Self:        self,
		Players:     players,
		ResumeToken: resumeToken,
		Policy:      room.policy,
	}
}

func (room *Room) broadcast(messageType string, payload any, exclude *Client) {
	data, err := protocol.NewMessage(messageType, "", payload)
	if err != nil {
		return
	}
	for _, current := range room.members {
		if current.client == nil || current.client == exclude {
			continue
		}
		if !current.client.enqueue(data) {
			go current.client.shutdown()
		}
	}
}
