package ws

import (
	"net"
	"testing"

	"github.com/rs/xid"
	"github.com/screego/server/ws/outgoing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestRooms() *Rooms {
	return &Rooms{
		Rooms:     map[string]*Room{},
		connected: map[xid.ID]string{},
	}
}

func newTestUser(authenticated bool, username string) ClientInfo {
	return ClientInfo{
		ID:                xid.New(),
		Authenticated:     authenticated,
		AuthenticatedUser: username,
		Write:             make(chan outgoing.Message, 10),
		Addr:              net.IPv4(127, 0, 0, 1),
	}
}

func addTestUserToRoom(rooms *Rooms, roomID string, user ClientInfo, name string, owner bool) {
	room, ok := rooms.Rooms[roomID]
	if !ok {
		room = &Room{
			ID:       roomID,
			Users:    map[xid.ID]*User{},
			Sessions: map[xid.ID]*RoomSession{},
		}
		rooms.Rooms[roomID] = room
	}
	room.Users[user.ID] = &User{
		ID:        user.ID,
		Name:      name,
		Streaming: false,
		Owner:     owner,
		Addr:      user.Addr,
		_write:    user.Write,
	}
	rooms.connected[user.ID] = roomID
}

func TestName_AuthenticatedUserCannotRename(t *testing.T) {
	rooms := newTestRooms()
	user := newTestUser(true, "alice")
	addTestUserToRoom(rooms, "room1", user, "alice", true)

	event := &Name{UserName: "mallory"}
	err := event.Execute(rooms, user)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "authenticated users cannot change their name")

	// Verify the name was NOT changed
	room := rooms.Rooms["room1"]
	assert.Equal(t, "alice", room.Users[user.ID].Name)
}

func TestName_GuestCanRename(t *testing.T) {
	rooms := newTestRooms()
	guest := newTestUser(false, "")
	addTestUserToRoom(rooms, "room1", guest, "GuestUser", false)

	event := &Name{UserName: "NewGuestName"}
	err := event.Execute(rooms, guest)

	require.NoError(t, err)

	room := rooms.Rooms["room1"]
	assert.Equal(t, "NewGuestName", room.Users[guest.ID].Name)
}

func TestName_NotInRoomReturnsError(t *testing.T) {
	rooms := newTestRooms()
	user := newTestUser(false, "")
	// Mark as connected but not in any room
	rooms.connected[user.ID] = ""

	event := &Name{UserName: "SomeName"}
	err := event.Execute(rooms, user)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not in a room")
}

func TestName_NotConnectedReturnsError(t *testing.T) {
	rooms := newTestRooms()
	user := newTestUser(false, "")
	// Not in connected map at all

	event := &Name{UserName: "SomeName"}
	err := event.Execute(rooms, user)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "not connected")
}

func TestName_AuthenticatedUserPreservesIdentityAcrossMultipleAttempts(t *testing.T) {
	rooms := newTestRooms()
	user := newTestUser(true, "bob")
	addTestUserToRoom(rooms, "room1", user, "bob", true)

	// Try multiple rename attempts
	for _, attemptedName := range []string{"alice", "admin", "", "bob-is-the-best"} {
		event := &Name{UserName: attemptedName}
		err := event.Execute(rooms, user)
		require.Error(t, err, "should reject rename attempt to %q", attemptedName)
	}

	// Name should still be "bob"
	room := rooms.Rooms["room1"]
	assert.Equal(t, "bob", room.Users[user.ID].Name)
}
