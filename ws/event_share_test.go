package ws

import (
	"errors"
	"math/rand"
	"net"
	"testing"
	"time"

	"github.com/rs/xid"
	"github.com/screego/server/config"
	"github.com/screego/server/config/ipdns"
	"github.com/screego/server/ws/outgoing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- mocks ---

type mockIPProvider struct {
	v4  net.IP
	v6  net.IP
	err error
}

func (m *mockIPProvider) Get() (net.IP, net.IP, error) {
	return m.v4, m.v6, m.err
}

type mockTurnServer struct{}

func (m *mockTurnServer) Credentials(id string, addr net.IP) (string, string) {
	return id, "password"
}

func (m *mockTurnServer) Disallow(username string) {}

// --- helpers ---

func newTestRooms(provider ipdns.Provider) *Rooms {
	return &Rooms{
		Rooms:      map[string]*Room{},
		Incoming:   make(chan ClientMessage, 10),
		connected:  map[xid.ID]string{},
		turnServer: &mockTurnServer{},
		config: config.Config{
			TurnIPProvider: provider,
			TurnPort:       "3478",
			AuthMode:       config.AuthModeNone,
		},
		r: rand.New(rand.NewSource(time.Now().Unix())),
	}
}

func newTestClient() ClientInfo {
	return ClientInfo{
		ID:    xid.New(),
		Write: make(chan outgoing.Message, 10),
		Addr:  net.ParseIP("127.0.0.1"),
	}
}

func createTestRoom(rooms *Rooms, owner ClientInfo, mode ConnectionMode) {
	create := &Create{
		ID:                "test-room",
		Mode:              mode,
		CloseOnOwnerLeave: true,
		UserName:          "owner",
	}
	_ = create.Execute(rooms, owner)
}

func joinTestRoom(rooms *Rooms, client ClientInfo) {
	join := &Join{
		ID:       "test-room",
		UserName: "viewer",
	}
	_ = join.Execute(rooms, client)
}

// drainWrite drains all messages from a client's write channel.
func drainWrite(ch chan outgoing.Message) []outgoing.Message {
	var msgs []outgoing.Message
	for {
		select {
		case m := <-ch:
			msgs = append(msgs, m)
		default:
			return msgs
		}
	}
}

// --- StartShare tests ---

func TestStartShare_TurnFailure_DoesNotDisconnect(t *testing.T) {
	provider := &mockIPProvider{err: errors.New("dns lookup timeout")}
	rooms := newTestRooms(provider)

	owner := newTestClient()
	createTestRoom(rooms, owner, ConnectionTURN)

	viewer := newTestClient()
	joinTestRoom(rooms, viewer)

	// Owner attempts to start sharing, TURN resolution fails
	share := &StartShare{}
	err := share.Execute(rooms, owner)

	// Execute should return nil (no error propagated to the dispatch loop)
	require.NoError(t, err, "StartShare should not return error on TURN failure")

	// Owner must still be connected
	_, connected := rooms.connected[owner.ID]
	assert.True(t, connected, "owner should still be connected after TURN failure")

	// Owner must still be in the room
	room := rooms.Rooms["test-room"]
	require.NotNil(t, room, "room should still exist")
	_, inRoom := room.Users[owner.ID]
	assert.True(t, inRoom, "owner should still be in the room after TURN failure")

	// Streaming must NOT be set to true
	assert.False(t, room.Users[owner.ID].Streaming,
		"Streaming should remain false when TURN resolution fails")

	// No sessions should be created
	assert.Empty(t, room.Sessions, "no sessions should be created on TURN failure")
}

func TestStartShare_TurnFailure_ViewerStaysInRoom(t *testing.T) {
	provider := &mockIPProvider{err: errors.New("dns lookup timeout")}
	rooms := newTestRooms(provider)

	owner := newTestClient()
	createTestRoom(rooms, owner, ConnectionTURN)

	viewer := newTestClient()
	joinTestRoom(rooms, viewer)

	// Owner attempts to start sharing, TURN resolution fails
	share := &StartShare{}
	_ = share.Execute(rooms, owner)

	// Viewer must still be connected and in the room
	_, connected := rooms.connected[viewer.ID]
	assert.True(t, connected, "viewer should still be connected")

	room := rooms.Rooms["test-room"]
	_, inRoom := room.Users[viewer.ID]
	assert.True(t, inRoom, "viewer should still be in the room")
}

func TestStartShare_RetryAfterTurnFailure(t *testing.T) {
	provider := &mockIPProvider{err: errors.New("dns lookup timeout")}
	rooms := newTestRooms(provider)

	owner := newTestClient()
	createTestRoom(rooms, owner, ConnectionTURN)

	viewer := newTestClient()
	joinTestRoom(rooms, viewer)

	// First attempt: TURN fails
	share := &StartShare{}
	err := share.Execute(rooms, owner)
	require.NoError(t, err)

	room := rooms.Rooms["test-room"]
	assert.False(t, room.Users[owner.ID].Streaming, "Streaming should be false after failed attempt")
	assert.Empty(t, room.Sessions, "no sessions after failed attempt")

	// Fix the TURN provider (simulating recovery)
	provider.err = nil
	provider.v4 = net.ParseIP("203.0.113.1")

	// Second attempt: should succeed
	err = share.Execute(rooms, owner)
	require.NoError(t, err)

	assert.True(t, room.Users[owner.ID].Streaming, "Streaming should be true after successful retry")
	assert.NotEmpty(t, room.Sessions, "sessions should be created after successful retry")
}

func TestStartShare_Success_CreatesSessionsForAllViewers(t *testing.T) {
	provider := &mockIPProvider{v4: net.ParseIP("203.0.113.1")}
	rooms := newTestRooms(provider)

	owner := newTestClient()
	createTestRoom(rooms, owner, ConnectionTURN)

	viewer1 := newTestClient()
	joinTestRoom(rooms, viewer1)

	viewer2 := newTestClient()
	joinTestRoom(rooms, viewer2)

	share := &StartShare{}
	err := share.Execute(rooms, owner)
	require.NoError(t, err)

	room := rooms.Rooms["test-room"]
	assert.True(t, room.Users[owner.ID].Streaming)
	assert.Len(t, room.Sessions, 2, "should create one session per viewer")
}

// --- Join tests ---

func TestJoin_TurnFailure_UserStillInRoom(t *testing.T) {
	// Use a working provider for the owner's create
	goodProvider := &mockIPProvider{v4: net.ParseIP("203.0.113.1")}
	rooms := newTestRooms(goodProvider)

	owner := newTestClient()
	createTestRoom(rooms, owner, ConnectionTURN)

	// Owner starts streaming
	share := &StartShare{}
	_ = share.Execute(rooms, owner)

	// Now switch to a failing provider for the join
	badProvider := &mockIPProvider{err: errors.New("dns lookup timeout")}
	rooms.config.TurnIPProvider = badProvider

	viewer := newTestClient()
	join := &Join{ID: "test-room", UserName: "viewer"}
	err := join.Execute(rooms, viewer)

	// Execute should return nil (no error propagated)
	require.NoError(t, err, "Join should not return error on TURN failure")

	// Viewer must still be connected
	_, connected := rooms.connected[viewer.ID]
	assert.True(t, connected, "viewer should still be connected after TURN failure during join")

	// Viewer must still be in the room
	room := rooms.Rooms["test-room"]
	require.NotNil(t, room)
	_, inRoom := room.Users[viewer.ID]
	assert.True(t, inRoom, "viewer should still be in the room after TURN failure during join")

	// No new sessions should be created for the viewer (since TURN failed)
	// The owner's existing sessions with other viewers should remain
	viewerSessions := 0
	for _, session := range room.Sessions {
		if session.Client == viewer.ID {
			viewerSessions++
		}
	}
	assert.Equal(t, 0, viewerSessions, "no sessions should be created for viewer when TURN fails")
}

// --- Rooms.Start dispatch tests ---

func TestRoomsStart_ShareTurnError_DoesNotTriggerDisconnect(t *testing.T) {
	provider := &mockIPProvider{err: errors.New("dns lookup timeout")}
	rooms := newTestRooms(provider)

	owner := newTestClient()
	createTestRoom(rooms, owner, ConnectionTURN)

	viewer := newTestClient()
	joinTestRoom(rooms, viewer)

	// Simulate the dispatch loop: send a share message through Incoming channel
	rooms.Incoming <- ClientMessage{Info: owner, Incoming: &StartShare{}}

	// Process one message from the dispatch loop
	// We need to run Start() briefly to process the message
	done := make(chan struct{})
	go func() {
		// Process just one message
		msg := <-rooms.Incoming
		if err := msg.Incoming.Execute(rooms, msg.Info); err != nil {
			dis := Disconnected{Reason: err.Error()}
			dis.executeNoError(rooms, msg.Info)
		}
		close(done)
	}()

	<-done

	// Verify no disconnect happened
	_, connected := rooms.connected[owner.ID]
	assert.True(t, connected, "owner should remain connected after TURN failure in dispatch loop")

	room := rooms.Rooms["test-room"]
	require.NotNil(t, room)
	_, inRoom := room.Users[owner.ID]
	assert.True(t, inRoom, "owner should remain in the room")
	assert.False(t, room.Users[owner.ID].Streaming, "Streaming should remain false")
}
