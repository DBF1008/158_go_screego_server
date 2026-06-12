package ws

import (
	"net"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/rs/xid"
	"github.com/screego/server/config"
	"github.com/screego/server/ws/outgoing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeTurnServer records credential allocation and release so tests can assert
// that TURN credentials are issued for every session and revoked when the
// session is torn down, regardless of which exit path closed it.
type fakeTurnServer struct {
	allowed    map[string]bool
	disallowed []string
}

func newFakeTurnServer() *fakeTurnServer {
	return &fakeTurnServer{allowed: map[string]bool{}}
}

func (f *fakeTurnServer) Credentials(id string, addr net.IP) (string, string) {
	f.allowed[id] = true
	return id, "pw-" + id
}

func (f *fakeTurnServer) Disallow(username string) {
	delete(f.allowed, username)
	f.disallowed = append(f.disallowed, username)
}

func newTestRooms() (*Rooms, *fakeTurnServer) {
	turnSrv := newFakeTurnServer()
	rooms := &Rooms{
		Rooms:      map[string]*Room{},
		connected:  map[xid.ID]string{},
		turnServer: turnSrv,
		config:     config.Config{TurnPort: "3478"},
	}
	return rooms, turnSrv
}

func newTestRoom(rooms *Rooms, id string, mode ConnectionMode, closeOnOwnerLeave bool) *Room {
	room := &Room{
		ID:                id,
		Mode:              mode,
		CloseOnOwnerLeave: closeOnOwnerLeave,
		Users:             map[xid.ID]*User{},
		Sessions:          map[xid.ID]*RoomSession{},
	}
	rooms.Rooms[id] = room
	return room
}

// addUser registers a user in the room and the rooms connection index, returning
// the user together with a buffered channel capturing every message written to it.
func addUser(rooms *Rooms, room *Room, name string, owner bool) (*User, chan outgoing.Message) {
	write := make(chan outgoing.Message, 32)
	user := &User{
		ID:     xid.New(),
		Addr:   net.IPv4(127, 0, 0, 1),
		Name:   name,
		Owner:  owner,
		_write: write,
	}
	room.Users[user.ID] = user
	rooms.connected[user.ID] = room.ID
	return user, write
}

// drain returns every message currently buffered on a user's write channel.
func drain(ch chan outgoing.Message) []outgoing.Message {
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

func hasEndShare(msgs []outgoing.Message, session xid.ID) bool {
	for _, m := range msgs {
		if es, ok := m.(outgoing.EndShare); ok && xid.ID(es) == session {
			return true
		}
	}
	return false
}

func countCloseWriter(msgs []outgoing.Message) int {
	n := 0
	for _, m := range msgs {
		if _, ok := m.(outgoing.CloseWriter); ok {
			n++
		}
	}
	return n
}

func sessionID(t *testing.T, room *Room, host, client xid.ID) xid.ID {
	t.Helper()
	for id, s := range room.Sessions {
		if s.Host == host && s.Client == client {
			return id
		}
	}
	t.Fatalf("no session found for host=%s client=%s", host, client)
	return xid.ID{}
}

var testV4 = net.IPv4(1, 2, 3, 4)

// Stopping a share must tear down only the sessions the user hosts, notify those
// clients, release their TURN credentials and bump the closed metric — while a
// session where the same user is a viewer (client) is left untouched.
func TestStopShareClosesOnlyHostSessions(t *testing.T) {
	rooms, turnSrv := newTestRooms()
	room := newTestRoom(rooms, "r1", ConnectionTURN, false)

	host, chHost := addUser(rooms, room, "host", true)
	clientA, chA := addUser(rooms, room, "a", false)
	clientB, chB := addUser(rooms, room, "b", false)

	// host streams to A and B; A also streams back to host (host is a viewer there).
	room.newSession(host.ID, clientA.ID, rooms, testV4, nil)
	room.newSession(host.ID, clientB.ID, rooms, testV4, nil)
	room.newSession(clientA.ID, host.ID, rooms, testV4, nil)
	host.Streaming = true
	clientA.Streaming = true

	idHostA := sessionID(t, room, host.ID, clientA.ID)
	idHostB := sessionID(t, room, host.ID, clientB.ID)
	idClientAHost := sessionID(t, room, clientA.ID, host.ID)

	drain(chHost)
	drain(chA)
	drain(chB)
	closedBefore := testutil.ToFloat64(sessionClosedTotal)

	require.NoError(t, (&StopShare{}).Execute(rooms, ClientInfo{ID: host.ID}))

	// the host's two outgoing sessions are gone; the viewing session survives.
	assert.NotContains(t, room.Sessions, idHostA)
	assert.NotContains(t, room.Sessions, idHostB)
	assert.Contains(t, room.Sessions, idClientAHost)
	assert.Len(t, room.Sessions, 1)

	// each affected client was notified that its share ended.
	assert.True(t, hasEndShare(drain(chA), idHostA))
	assert.True(t, hasEndShare(drain(chB), idHostB))

	assert.Equal(t, closedBefore+2, testutil.ToFloat64(sessionClosedTotal))
	assert.False(t, room.Users[host.ID].Streaming)

	// TURN credentials of the closed sessions are revoked; the survivor keeps its.
	assert.Contains(t, turnSrv.disallowed, idHostA.String()+"host")
	assert.Contains(t, turnSrv.disallowed, idHostA.String()+"client")
	assert.Contains(t, turnSrv.disallowed, idHostB.String()+"host")
	assert.Contains(t, turnSrv.disallowed, idHostB.String()+"client")
	assert.NotContains(t, turnSrv.disallowed, idClientAHost.String()+"host")
	assert.NotContains(t, turnSrv.disallowed, idClientAHost.String()+"client")
}

// A viewer disconnecting must notify the host of the abandoned session, close it,
// release its credentials and record the metric, leaving the room intact.
func TestDisconnectClientNotifiesHost(t *testing.T) {
	rooms, turnSrv := newTestRooms()
	room := newTestRoom(rooms, "r1", ConnectionTURN, false)

	host, chHost := addUser(rooms, room, "host", true)
	client, chClient := addUser(rooms, room, "client", false)

	room.newSession(host.ID, client.ID, rooms, testV4, nil)
	host.Streaming = true
	id := sessionID(t, room, host.ID, client.ID)
	drain(chHost)
	drain(chClient)

	closedBefore := testutil.ToFloat64(sessionClosedTotal)
	leftBefore := testutil.ToFloat64(usersLeftTotal)

	(&Disconnected{Code: websocket.CloseNormalClosure, Reason: "bye"}).
		executeNoError(rooms, ClientInfo{ID: client.ID, Write: chClient})

	assert.True(t, hasEndShare(drain(chHost), id))
	assert.Empty(t, room.Sessions)
	assert.NotContains(t, room.Users, client.ID)
	assert.Contains(t, rooms.Rooms, "r1")

	assert.Equal(t, closedBefore+1, testutil.ToFloat64(sessionClosedTotal))
	assert.Equal(t, leftBefore+1, testutil.ToFloat64(usersLeftTotal))
	assert.Contains(t, turnSrv.disallowed, id.String()+"host")
	assert.Contains(t, turnSrv.disallowed, id.String()+"client")
}

// A host disconnecting must notify every viewer, tear down all its sessions and
// release their credentials, while the room (and remaining users) stays alive.
func TestDisconnectHostNotifiesAllClients(t *testing.T) {
	rooms, turnSrv := newTestRooms()
	room := newTestRoom(rooms, "r1", ConnectionTURN, false)

	host, chHost := addUser(rooms, room, "host", false)
	a, chA := addUser(rooms, room, "a", true)
	b, chB := addUser(rooms, room, "b", false)

	room.newSession(host.ID, a.ID, rooms, testV4, nil)
	room.newSession(host.ID, b.ID, rooms, testV4, nil)
	host.Streaming = true
	idA := sessionID(t, room, host.ID, a.ID)
	idB := sessionID(t, room, host.ID, b.ID)
	drain(chHost)
	drain(chA)
	drain(chB)

	closedBefore := testutil.ToFloat64(sessionClosedTotal)

	(&Disconnected{Code: websocket.CloseNormalClosure, Reason: "bye"}).
		executeNoError(rooms, ClientInfo{ID: host.ID, Write: chHost})

	assert.True(t, hasEndShare(drain(chA), idA))
	assert.True(t, hasEndShare(drain(chB), idB))
	assert.Empty(t, room.Sessions)
	assert.Contains(t, rooms.Rooms, "r1")
	assert.NotContains(t, room.Users, host.ID)

	assert.Equal(t, closedBefore+2, testutil.ToFloat64(sessionClosedTotal))
	assert.Contains(t, turnSrv.disallowed, idA.String()+"host")
	assert.Contains(t, turnSrv.disallowed, idA.String()+"client")
	assert.Contains(t, turnSrv.disallowed, idB.String()+"host")
	assert.Contains(t, turnSrv.disallowed, idB.String()+"client")
}

// When the owner leaves a CloseOnOwnerLeave room, the owner's own sessions are
// closed with an EndShare to their peers, but the remaining session between the
// other members is torn down by closeRoom WITHOUT an EndShare (those members get
// a CloseWriter instead). All credentials are still released and metrics recorded
// — this guards the divergence between the per-user path and the room-close path.
func TestOwnerLeaveClosesRoomWithoutPeerNotifyForRoomSessions(t *testing.T) {
	rooms, turnSrv := newTestRooms()
	room := newTestRoom(rooms, "r1", ConnectionTURN, true)

	owner, chOwner := addUser(rooms, room, "owner", true)
	a, chA := addUser(rooms, room, "a", false)
	b, chB := addUser(rooms, room, "b", false)

	room.newSession(owner.ID, a.ID, rooms, testV4, nil)
	room.newSession(owner.ID, b.ID, rooms, testV4, nil)
	room.newSession(a.ID, b.ID, rooms, testV4, nil) // unrelated to the owner
	owner.Streaming = true
	a.Streaming = true
	idOwnerA := sessionID(t, room, owner.ID, a.ID)
	idOwnerB := sessionID(t, room, owner.ID, b.ID)
	idAB := sessionID(t, room, a.ID, b.ID)
	drain(chOwner)
	drain(chA)
	drain(chB)

	closedBefore := testutil.ToFloat64(sessionClosedTotal)
	roomsClosedBefore := testutil.ToFloat64(roomsClosedTotal)
	leftBefore := testutil.ToFloat64(usersLeftTotal)

	(&Disconnected{Code: websocket.CloseNormalClosure, Reason: "bye"}).
		executeNoError(rooms, ClientInfo{ID: owner.ID, Write: chOwner})

	msgsA := drain(chA)
	msgsB := drain(chB)

	// the owner's sessions notified their peers...
	assert.True(t, hasEndShare(msgsA, idOwnerA))
	assert.True(t, hasEndShare(msgsB, idOwnerB))
	// ...but the unrelated A<->B session is closed by closeRoom without EndShare,
	// the members receive exactly one CloseWriter instead.
	assert.False(t, hasEndShare(msgsA, idAB))
	assert.False(t, hasEndShare(msgsB, idAB))
	assert.Equal(t, 1, countCloseWriter(msgsA))
	assert.Equal(t, 1, countCloseWriter(msgsB))

	assert.NotContains(t, rooms.Rooms, "r1")
	assert.Equal(t, closedBefore+3, testutil.ToFloat64(sessionClosedTotal))
	assert.Equal(t, roomsClosedBefore+1, testutil.ToFloat64(roomsClosedTotal))
	assert.Equal(t, leftBefore+3, testutil.ToFloat64(usersLeftTotal))

	// every credential issued for the room has been revoked.
	assert.Empty(t, turnSrv.allowed)
	for _, id := range []xid.ID{idOwnerA, idOwnerB, idAB} {
		assert.Contains(t, turnSrv.disallowed, id.String()+"host")
		assert.Contains(t, turnSrv.disallowed, id.String()+"client")
	}
}

// The last user leaving an ordinary room closes it and records the room metric.
func TestDisconnectLastUserClosesEmptyRoom(t *testing.T) {
	rooms, _ := newTestRooms()
	room := newTestRoom(rooms, "r1", ConnectionLocal, false)
	user, chUser := addUser(rooms, room, "solo", true)

	roomsClosedBefore := testutil.ToFloat64(roomsClosedTotal)

	(&Disconnected{Code: websocket.CloseNormalClosure, Reason: "bye"}).
		executeNoError(rooms, ClientInfo{ID: user.ID, Write: chUser})

	assert.NotContains(t, rooms.Rooms, "r1")
	assert.Equal(t, roomsClosedBefore+1, testutil.ToFloat64(roomsClosedTotal))
}
