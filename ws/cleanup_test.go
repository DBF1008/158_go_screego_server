package ws

import (
	"net"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"
	"github.com/screego/server/config"
	"github.com/screego/server/config/ipdns"
	"github.com/screego/server/ws/outgoing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock TURN server
// ---------------------------------------------------------------------------

type mockTurnServer struct {
	disallowed []string
}

func (m *mockTurnServer) Credentials(id string, addr net.IP) (string, string) {
	return id, "pw-" + id
}

func (m *mockTurnServer) Disallow(username string) {
	m.disallowed = append(m.disallowed, username)
}

// noopTurnServer is used when TURN mode is not being tested.
type noopTurnServer struct{}

func (n *noopTurnServer) Credentials(id string, addr net.IP) (string, string) {
	return id, "pw"
}
func (n *noopTurnServer) Disallow(string) {}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

// testUser wraps a User and keeps a readable reference to the message channel
// for assertions. The User._write field is send-only (chan<-).
type testUser struct {
	*User
	ch   chan outgoing.Message // bidirectional, for both reading and writing
	msgs <-chan outgoing.Message
}

// newTestUser creates a User with a buffered write channel suitable for tests.
func newTestUser(t *testing.T, name string, owner bool) *testUser {
	t.Helper()
	ch := make(chan outgoing.Message, 100)
	return &testUser{
		User: &User{
			ID:     xid.New(),
			Addr:   net.ParseIP("127.0.0.1"),
			Name:   name,
			Owner:  owner,
			_write: ch,
		},
		ch:   ch,
		msgs: ch,
	}
}

// newTestRooms creates a Rooms with a mockTurnServer for testing.
func newTestRooms(t *testing.T, turnMode bool) (*Rooms, *mockTurnServer) {
	t.Helper()
	mock := &mockTurnServer{}
	conf := config.Config{
		TurnIPProvider: &ipdns.Static{V4: net.ParseIP("127.0.0.1")},
		TurnPort:       "3478",
	}
	r := &Rooms{
		Rooms:     map[string]*Room{},
		Incoming:  make(chan ClientMessage, 100),
		connected: map[xid.ID]string{},
		config:    conf,
	}
	if turnMode {
		r.turnServer = mock
	} else {
		r.turnServer = &noopTurnServer{}
	}
	return r, mock
}

// setupRoom creates a room with the given mode and registers it in Rooms.
func setupRoom(t *testing.T, rooms *Rooms, roomID string, mode ConnectionMode, closeOnOwnerLeave bool) *Room {
	t.Helper()
	room := &Room{
		ID:                roomID,
		CloseOnOwnerLeave: closeOnOwnerLeave,
		Mode:              mode,
		Users:             map[xid.ID]*User{},
		Sessions:          map[xid.ID]*RoomSession{},
	}
	rooms.Rooms[roomID] = room
	return room
}

// addUser adds a testUser to the room and registers them in rooms.connected.
func addUser(t *testing.T, rooms *Rooms, room *Room, tu *testUser) {
	t.Helper()
	room.Users[tu.ID] = tu.User
	rooms.connected[tu.ID] = room.ID
}

// createSession creates a session between host and client in the room.
func createSession(t *testing.T, rooms *Rooms, room *Room, host, client *testUser) xid.ID {
	t.Helper()
	id := xid.New()
	room.Sessions[id] = &RoomSession{Host: host.ID, Client: client.ID}
	sessionCreatedTotal.Inc()
	if room.Mode == ConnectionTURN {
		rooms.turnServer.Credentials(id.String()+"host", host.Addr)
		rooms.turnServer.Credentials(id.String()+"client", client.Addr)
	}
	return id
}

// drainMessages reads all pending messages from a testUser's channel.
func drainMessages(tu *testUser) []outgoing.Message {
	var msgs []outgoing.Message
	for {
		select {
		case m := <-tu.msgs:
			msgs = append(msgs, m)
		default:
			return msgs
		}
	}
}

// containsEndShare checks if any message is an EndShare with the given ID.
func containsEndShare(msgs []outgoing.Message, sessionID xid.ID) bool {
	for _, m := range msgs {
		if es, ok := m.(outgoing.EndShare); ok && xid.ID(es) == sessionID {
			return true
		}
	}
	return false
}

// containsCloseWriter checks if any message is a CloseWriter.
func containsCloseWriter(msgs []outgoing.Message) bool {
	for _, m := range msgs {
		if _, ok := m.(outgoing.CloseWriter); ok {
			return true
		}
	}
	return false
}

// sessionClosedDelta returns a function that reports how many times
// sessionClosedTotal was incremented since the snapshot was taken.
func sessionClosedDelta() func() float64 {
	before := testutil.ToFloat64(sessionClosedTotal)
	return func() float64 {
		return testutil.ToFloat64(sessionClosedTotal) - before
	}
}

// ---------------------------------------------------------------------------
// StopShare path tests
// ---------------------------------------------------------------------------

func TestStopShare_ClosesHostSessionsOnly(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

	host := newTestUser(t, "host", true)
	client := newTestUser(t, "client", false)
	otherHost := newTestUser(t, "otherhost", false)

	addUser(t, rooms, room, host)
	addUser(t, rooms, room, client)
	addUser(t, rooms, room, otherHost)

	// host is sharing -> session(host->client)
	s1 := createSession(t, rooms, room, host, client)
	host.Streaming = true

	// otherHost is also sharing -> session(otherHost->host)
	s2 := createSession(t, rooms, room, otherHost, host)
	otherHost.Streaming = true

	assert.Len(t, room.Sessions, 2)

	// host stops sharing
	host.Streaming = false
	room.closeHostSessions(rooms, host.ID)

	// s1 (host->client) should be closed; s2 (otherHost->host) should survive
	assert.Len(t, room.Sessions, 1)
	_, s1Exists := room.Sessions[s1]
	_, s2Exists := room.Sessions[s2]
	assert.False(t, s1Exists, "host's session should be closed")
	assert.True(t, s2Exists, "other user's session should survive")

	// client should receive EndShare for s1
	msgs := drainMessages(client)
	assert.True(t, containsEndShare(msgs, s1), "client should receive EndShare for closed session")

	// host should NOT receive EndShare for s2 (closeHostSessions only targets host sessions)
	hostMsgs := drainMessages(host)
	assert.False(t, containsEndShare(hostMsgs, s2), "host should not receive EndShare for other's session")
}

func TestStopShare_NotifiesClientPeers(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

	host := newTestUser(t, "host", true)
	c1 := newTestUser(t, "c1", false)
	c2 := newTestUser(t, "c2", false)

	addUser(t, rooms, room, host)
	addUser(t, rooms, room, c1)
	addUser(t, rooms, room, c2)

	s1 := createSession(t, rooms, room, host, c1)
	s2 := createSession(t, rooms, room, host, c2)

	room.closeHostSessions(rooms, host.ID)

	c1Msgs := drainMessages(c1)
	c2Msgs := drainMessages(c2)
	assert.True(t, containsEndShare(c1Msgs, s1), "c1 should receive EndShare")
	assert.True(t, containsEndShare(c2Msgs, s2), "c2 should receive EndShare")
}

func TestStopShare_ReleasesTURNCredentials(t *testing.T) {
	rooms, mock := newTestRooms(t, true)
	room := setupRoom(t, rooms, "r1", ConnectionTURN, false)

	host := newTestUser(t, "host", true)
	client := newTestUser(t, "client", false)

	addUser(t, rooms, room, host)
	addUser(t, rooms, room, client)

	sID := createSession(t, rooms, room, host, client)

	room.closeHostSessions(rooms, host.ID)

	assert.Contains(t, mock.disallowed, sID.String()+"host", "host TURN cred should be released")
	assert.Contains(t, mock.disallowed, sID.String()+"client", "client TURN cred should be released")
}

func TestStopShare_MetricsIncrement(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

	host := newTestUser(t, "host", true)
	client := newTestUser(t, "client", false)

	addUser(t, rooms, room, host)
	addUser(t, rooms, room, client)

	createSession(t, rooms, room, host, client)
	createSession(t, rooms, room, host, client) // 2 sessions same pair

	delta := sessionClosedDelta()
	room.closeHostSessions(rooms, host.ID)

	assert.Equal(t, float64(2), delta(), "sessionClosedTotal should increment by 2")
}

// ---------------------------------------------------------------------------
// Disconnected path tests
// ---------------------------------------------------------------------------

func TestDisconnect_AsHost_ClosesSessions(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

	host := newTestUser(t, "host", true)
	client := newTestUser(t, "client", false)

	addUser(t, rooms, room, host)
	addUser(t, rooms, room, client)

	sID := createSession(t, rooms, room, host, client)

	// Simulate disconnect: user removed before closeUserSessions
	delete(room.Users, host.ID)
	delete(rooms.connected, host.ID)
	room.closeUserSessions(rooms, host.ID)

	assert.Empty(t, room.Sessions, "all host sessions should be closed")

	clientMsgs := drainMessages(client)
	assert.True(t, containsEndShare(clientMsgs, sID), "client should receive EndShare")
}

func TestDisconnect_AsClient_ClosesSessions(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

	host := newTestUser(t, "host", true)
	client := newTestUser(t, "client", false)

	addUser(t, rooms, room, host)
	addUser(t, rooms, room, client)

	sID := createSession(t, rooms, room, host, client)

	// Simulate disconnect
	delete(room.Users, client.ID)
	delete(rooms.connected, client.ID)
	room.closeUserSessions(rooms, client.ID)

	assert.Empty(t, room.Sessions, "all client sessions should be closed")

	hostMsgs := drainMessages(host)
	assert.True(t, containsEndShare(hostMsgs, sID), "host should receive EndShare")
}

func TestDisconnect_NonStreamingUser_NoSessionsAffected(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

	host := newTestUser(t, "host", true)
	client := newTestUser(t, "client", false)
	viewer := newTestUser(t, "viewer", false)

	addUser(t, rooms, room, host)
	addUser(t, rooms, room, client)
	addUser(t, rooms, room, viewer)

	sID := createSession(t, rooms, room, host, client)

	// viewer disconnects -- no sessions
	delete(room.Users, viewer.ID)
	delete(rooms.connected, viewer.ID)
	room.closeUserSessions(rooms, viewer.ID)

	assert.Len(t, room.Sessions, 1, "existing session should survive")
	_, exists := room.Sessions[sID]
	assert.True(t, exists)
}

func TestDisconnect_OwnerLeave_CloseOnOwnerLeave(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, true) // CloseOnOwnerLeave

	owner := newTestUser(t, "owner", true)
	member1 := newTestUser(t, "member1", false)
	member2 := newTestUser(t, "member2", false)

	addUser(t, rooms, room, owner)
	addUser(t, rooms, room, member1)
	addUser(t, rooms, room, member2)

	s1 := createSession(t, rooms, room, owner, member1)
	s2 := createSession(t, rooms, room, owner, member2)

	delta := sessionClosedDelta()

	// Simulate the full disconnect path for the owner
	delete(rooms.connected, owner.ID)
	writeTimeout[outgoing.Message](owner.ch, outgoing.CloseWriter{Code: websocket.CloseNormalClosure, Reason: "done"})
	delete(room.Users, owner.ID)
	usersLeftTotal.Inc()

	room.closeUserSessions(rooms, owner.ID)

	// Both sessions closed, members notified
	assert.Empty(t, room.Sessions, "all owner sessions should be closed")
	assert.Equal(t, float64(2), delta())

	m1Msgs := drainMessages(member1)
	m2Msgs := drainMessages(member2)
	assert.True(t, containsEndShare(m1Msgs, s1))
	assert.True(t, containsEndShare(m2Msgs, s2))

	// Now simulate CloseOnOwnerLeave cascade
	for _, member := range room.Users {
		delete(rooms.connected, member.ID)
		member.WriteTimeout(outgoing.CloseWriter{Code: websocket.CloseNormalClosure, Reason: CloseOwnerLeft})
	}
	rooms.closeRoom("r1")

	_, roomExists := rooms.Rooms["r1"]
	assert.False(t, roomExists, "room should be deleted after owner leaves")

	m1Msgs = drainMessages(member1)
	m2Msgs = drainMessages(member2)
	assert.True(t, containsCloseWriter(m1Msgs))
	assert.True(t, containsCloseWriter(m2Msgs))
}

func TestDisconnect_LastUser_RoomClosed(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

	user := newTestUser(t, "user", true)
	addUser(t, rooms, room, user)

	delete(rooms.connected, user.ID)
	delete(room.Users, user.ID)
	usersLeftTotal.Inc()

	room.closeUserSessions(rooms, user.ID)

	if len(room.Users) == 0 {
		rooms.closeRoom("r1")
	}

	_, roomExists := rooms.Rooms["r1"]
	assert.False(t, roomExists, "room should be deleted when last user leaves")
}

func TestDisconnect_MultipleHostAndClientSessions(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

	user := newTestUser(t, "user", false)
	host1 := newTestUser(t, "host1", true)
	host2 := newTestUser(t, "host2", false)
	client1 := newTestUser(t, "client1", false)

	addUser(t, rooms, room, user)
	addUser(t, rooms, room, host1)
	addUser(t, rooms, room, host2)
	addUser(t, rooms, room, client1)

	// user is host for one session and client for another
	sAsHost := createSession(t, rooms, room, user, client1)
	sAsClient := createSession(t, rooms, room, host1, user)
	// unrelated session survives
	sUnrelated := createSession(t, rooms, room, host2, client1)

	delete(room.Users, user.ID)
	delete(rooms.connected, user.ID)
	room.closeUserSessions(rooms, user.ID)

	assert.Len(t, room.Sessions, 1, "only the unrelated session should survive")
	_, exists := room.Sessions[sUnrelated]
	assert.True(t, exists)
	_, hostExists := room.Sessions[sAsHost]
	_, clientExists := room.Sessions[sAsClient]
	assert.False(t, hostExists)
	assert.False(t, clientExists)

	c1Msgs := drainMessages(client1)
	assert.True(t, containsEndShare(c1Msgs, sAsHost))

	h1Msgs := drainMessages(host1)
	assert.True(t, containsEndShare(h1Msgs, sAsClient))
}

// ---------------------------------------------------------------------------
// closeRoom path tests
// ---------------------------------------------------------------------------

func TestCloseRoom_ClosesAllSessions(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

	h1 := newTestUser(t, "h1", true)
	c1 := newTestUser(t, "c1", false)
	h2 := newTestUser(t, "h2", false)
	c2 := newTestUser(t, "c2", false)

	addUser(t, rooms, room, h1)
	addUser(t, rooms, room, c1)
	addUser(t, rooms, room, h2)
	addUser(t, rooms, room, c2)

	createSession(t, rooms, room, h1, c1)
	createSession(t, rooms, room, h2, c2)

	delta := sessionClosedDelta()
	rooms.closeRoom("r1")

	assert.Equal(t, float64(2), delta(), "sessionClosedTotal should increment by 2")
	_, exists := rooms.Rooms["r1"]
	assert.False(t, exists, "room should be deleted")
}

func TestCloseRoom_NotifiesAllPeers(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

	host := newTestUser(t, "host", true)
	client := newTestUser(t, "client", false)

	addUser(t, rooms, room, host)
	addUser(t, rooms, room, client)

	sID := createSession(t, rooms, room, host, client)

	rooms.closeRoom("r1")

	hostMsgs := drainMessages(host)
	clientMsgs := drainMessages(client)
	assert.True(t, containsEndShare(hostMsgs, sID), "host should receive EndShare on room close")
	assert.True(t, containsEndShare(clientMsgs, sID), "client should receive EndShare on room close")
}

func TestCloseRoom_EmptyRoom(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	setupRoom(t, rooms, "r1", ConnectionLocal, false)

	// closeRoom on an empty room should not panic
	rooms.closeRoom("r1")

	_, exists := rooms.Rooms["r1"]
	assert.False(t, exists)
}

func TestCloseRoom_AlreadyDeleted(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	// closeRoom on a non-existent room should not panic
	rooms.closeRoom("nonexistent")
}

func TestCloseRoom_ReleasesTURNCredentials(t *testing.T) {
	rooms, mock := newTestRooms(t, true)
	room := setupRoom(t, rooms, "r1", ConnectionTURN, false)

	host := newTestUser(t, "host", true)
	client := newTestUser(t, "client", false)

	addUser(t, rooms, room, host)
	addUser(t, rooms, room, client)

	s1 := createSession(t, rooms, room, host, client)
	s2 := createSession(t, rooms, room, host, client)

	rooms.closeRoom("r1")

	// Each session: host + client credentials = 4 Disallow calls total
	require.Len(t, mock.disallowed, 4)
	assert.Contains(t, mock.disallowed, s1.String()+"host")
	assert.Contains(t, mock.disallowed, s1.String()+"client")
	assert.Contains(t, mock.disallowed, s2.String()+"host")
	assert.Contains(t, mock.disallowed, s2.String()+"client")
}

func TestCloseRoom_MetricsConsistency(t *testing.T) {
	rooms, _ := newTestRooms(t, false)
	room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

	h1 := newTestUser(t, "h1", true)
	c1 := newTestUser(t, "c1", false)
	addUser(t, rooms, room, h1)
	addUser(t, rooms, room, c1)

	createSession(t, rooms, room, h1, c1)

	usersBefore := testutil.ToFloat64(usersLeftTotal)
	sessBefore := testutil.ToFloat64(sessionClosedTotal)
	roomsBefore := testutil.ToFloat64(roomsClosedTotal)

	rooms.closeRoom("r1")

	assert.Equal(t, float64(2), testutil.ToFloat64(usersLeftTotal)-usersBefore)
	assert.Equal(t, float64(1), testutil.ToFloat64(sessionClosedTotal)-sessBefore)
	assert.Equal(t, float64(1), testutil.ToFloat64(roomsClosedTotal)-roomsBefore)
}

// ---------------------------------------------------------------------------
// Cross-path consistency: TURN credential release
// ---------------------------------------------------------------------------

func TestTURNRelease_AcrossPaths(t *testing.T) {
	tests := []struct {
		name   string
		action func(t *testing.T, rooms *Rooms, room *Room, host, client *testUser)
	}{
		{
			name: "StopShare",
			action: func(t *testing.T, rooms *Rooms, room *Room, host, client *testUser) {
				room.closeHostSessions(rooms, host.ID)
			},
		},
		{
			name: "Disconnect",
			action: func(t *testing.T, rooms *Rooms, room *Room, host, client *testUser) {
				delete(room.Users, host.ID)
				delete(rooms.connected, host.ID)
				room.closeUserSessions(rooms, host.ID)
			},
		},
		{
			name: "CloseRoom",
			action: func(t *testing.T, rooms *Rooms, room *Room, host, client *testUser) {
				rooms.closeRoom("r1")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rooms, mock := newTestRooms(t, true)
			room := setupRoom(t, rooms, "r1", ConnectionTURN, false)

			host := newTestUser(t, "host", true)
			client := newTestUser(t, "client", false)

			addUser(t, rooms, room, host)
			addUser(t, rooms, room, client)

			sID := createSession(t, rooms, room, host, client)

			tt.action(t, rooms, room, host, client)

			assert.Contains(t, mock.disallowed, sID.String()+"host",
				"host TURN cred should be released")
			assert.Contains(t, mock.disallowed, sID.String()+"client",
				"client TURN cred should be released")
		})
	}
}

// ---------------------------------------------------------------------------
// Cross-path consistency: EndShare notification
// ---------------------------------------------------------------------------

func TestEndShareNotification_AcrossPaths(t *testing.T) {
	tests := []struct {
		name          string
		action        func(t *testing.T, rooms *Rooms, room *Room, host, client *testUser)
		expectHostMsg bool
		expectClntMsg bool
	}{
		{
			name: "StopShare_notifiesClientOnly",
			action: func(t *testing.T, rooms *Rooms, room *Room, host, client *testUser) {
				room.closeHostSessions(rooms, host.ID)
			},
			expectHostMsg: false,
			expectClntMsg: true,
		},
		{
			name: "Disconnect_hostLeaves_notifiesClient",
			action: func(t *testing.T, rooms *Rooms, room *Room, host, client *testUser) {
				delete(room.Users, host.ID)
				delete(rooms.connected, host.ID)
				room.closeUserSessions(rooms, host.ID)
			},
			expectHostMsg: false, // host already removed from Users
			expectClntMsg: true,
		},
		{
			name: "CloseRoom_notifiesBoth",
			action: func(t *testing.T, rooms *Rooms, room *Room, host, client *testUser) {
				rooms.closeRoom("r1")
			},
			expectHostMsg: true,
			expectClntMsg: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rooms, _ := newTestRooms(t, false)
			room := setupRoom(t, rooms, "r1", ConnectionLocal, false)

			host := newTestUser(t, "host", true)
			client := newTestUser(t, "client", false)

			addUser(t, rooms, room, host)
			addUser(t, rooms, room, client)

			sID := createSession(t, rooms, room, host, client)

			tt.action(t, rooms, room, host, client)

			hostMsgs := drainMessages(host)
			clientMsgs := drainMessages(client)

			if tt.expectHostMsg {
				assert.True(t, containsEndShare(hostMsgs, sID),
					"host should receive EndShare")
			} else {
				assert.False(t, containsEndShare(hostMsgs, sID),
					"host should NOT receive EndShare")
			}

			if tt.expectClntMsg {
				assert.True(t, containsEndShare(clientMsgs, sID),
					"client should receive EndShare")
			} else {
				assert.False(t, containsEndShare(clientMsgs, sID),
					"client should NOT receive EndShare")
			}
		})
	}
}
