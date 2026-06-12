package ws

import (
	"testing"

	"github.com/rs/xid"
	"github.com/screego/server/ws/outgoing"
)

// newNameTestRoom builds a single-user room wired up enough to execute the
// name event. The user's write channel is buffered so notifyInfoChanged does
// not block when a rename is broadcast.
func newNameTestRoom(name string) (*Rooms, *Room, xid.ID) {
	id := xid.New()
	write := make(chan outgoing.Message, 8)
	room := &Room{
		ID:       "room",
		Users:    map[xid.ID]*User{id: {ID: id, Name: name, _write: write}},
		Sessions: map[xid.ID]*RoomSession{},
	}
	rooms := &Rooms{
		Rooms:     map[string]*Room{room.ID: room},
		connected: map[xid.ID]string{id: room.ID},
	}
	return rooms, room, id
}

// TestNameEventAuthenticatedUserKeepsIdentity verifies that a logged-in user
// cannot change their display name via a name event: it stays bound to their
// authenticated identity even when an arbitrary username is sent.
func TestNameEventAuthenticatedUserKeepsIdentity(t *testing.T) {
	rooms, room, id := newNameTestRoom("alice")
	current := ClientInfo{ID: id, Authenticated: true, AuthenticatedUser: "alice"}

	if err := (&Name{UserName: "spoofed"}).Execute(rooms, current); err != nil {
		t.Fatalf("Name.Execute returned error: %v", err)
	}

	if got := room.Users[id].Name; got != "alice" {
		t.Fatalf("authenticated user name = %q, want it pinned to %q", got, "alice")
	}
}

// TestNameEventGuestCanRename verifies that guests (not authenticated) can
// still freely change their display name.
func TestNameEventGuestCanRename(t *testing.T) {
	rooms, room, id := newNameTestRoom("Old Guest Name")
	current := ClientInfo{ID: id, Authenticated: false, AuthenticatedUser: "guest"}

	if err := (&Name{UserName: "New Guest Name"}).Execute(rooms, current); err != nil {
		t.Fatalf("Name.Execute returned error: %v", err)
	}

	if got := room.Users[id].Name; got != "New Guest Name" {
		t.Fatalf("guest user name = %q, want %q", got, "New Guest Name")
	}
}
