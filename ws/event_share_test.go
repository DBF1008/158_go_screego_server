package ws

import (
	"errors"
	"net"
	"testing"

	"github.com/rs/xid"
	"github.com/screego/server/config"
	"github.com/screego/server/ws/outgoing"
)

// fakeIPProvider is a configurable ipdns.Provider used to simulate a transient
// failure (and its later recovery) when resolving the external TURN/STUN
// address.
type fakeIPProvider struct {
	v4  net.IP
	v6  net.IP
	err error
}

func (f *fakeIPProvider) Get() (net.IP, net.IP, error) {
	return f.v4, f.v6, f.err
}

// newShareTestRoom builds a room that already has two collaborating users (an
// owner/host and a peer) connected, mirroring the "someone is already in the
// room" situation in which a share is started. ConnectionLocal keeps newSession
// free of any real TURN server dependency.
func newShareTestRoom(provider *fakeIPProvider) (*Rooms, *Room, *User, *User, ClientInfo) {
	host := &User{
		ID:     xid.New(),
		Name:   "host",
		Owner:  true,
		Addr:   net.IPv4(127, 0, 0, 1),
		_write: make(chan outgoing.Message, 16),
	}
	peer := &User{
		ID:     xid.New(),
		Name:   "peer",
		Owner:  false,
		Addr:   net.IPv4(127, 0, 0, 1),
		_write: make(chan outgoing.Message, 16),
	}

	room := &Room{
		ID:       "room",
		Mode:     ConnectionLocal,
		Users:    map[xid.ID]*User{host.ID: host, peer.ID: peer},
		Sessions: map[xid.ID]*RoomSession{},
	}

	rooms := &Rooms{
		Rooms:     map[string]*Room{room.ID: room},
		connected: map[xid.ID]string{host.ID: room.ID, peer.ID: room.ID},
		config:    config.Config{TurnIPProvider: provider, TurnPort: "3478"},
	}

	return rooms, room, host, peer, ClientInfo{ID: host.ID, Addr: host.Addr}
}

// TestStartShareTurnFailureKeepsConnectionAndRoom verifies that a transient
// failure while resolving the TURN address during "start share" is contained to
// that single attempt: it must not be reported as a fatal error (which
// Rooms.Start turns into a WebSocket disconnect), must not flip the requesting
// user to streaming, and must leave room membership and sessions untouched so
// the room keeps working for everyone.
func TestStartShareTurnFailureKeepsConnectionAndRoom(t *testing.T) {
	provider := &fakeIPProvider{err: errors.New("transient turn address resolution failure")}
	rooms, room, host, peer, hostInfo := newShareTestRoom(provider)

	err := (&StartShare{}).Execute(rooms, hostInfo)

	if err != nil {
		t.Fatalf("start share returned a fatal error %q; a transient TURN failure must not drop the WebSocket", err)
	}
	if host.Streaming {
		t.Error("host was left marked as streaming after a failed share; state was not rolled back")
	}
	if got := len(room.Users); got != 2 {
		t.Errorf("room lost members after failed share: got %d users, want 2", got)
	}
	if got := len(room.Sessions); got != 0 {
		t.Errorf("failed share created %d sessions, want 0", got)
	}
	if _, ok := rooms.connected[host.ID]; !ok {
		t.Error("host was disconnected from the room after a failed share")
	}
	if _, ok := rooms.connected[peer.ID]; !ok {
		t.Error("peer was disconnected from the room after another user's failed share")
	}
}

// TestStartShareSucceedsOnRetryAfterTransientFailure verifies that once the
// transient failure clears, retrying the share works normally. This proves the
// failed attempt left no residual state that would block a later share.
func TestStartShareSucceedsOnRetryAfterTransientFailure(t *testing.T) {
	provider := &fakeIPProvider{err: errors.New("transient turn address resolution failure")}
	rooms, room, host, _, hostInfo := newShareTestRoom(provider)

	if err := (&StartShare{}).Execute(rooms, hostInfo); err != nil {
		t.Fatalf("first (failing) share returned a fatal error: %v", err)
	}
	if host.Streaming || len(room.Sessions) != 0 {
		t.Fatalf("failed share left residue: streaming=%v sessions=%d", host.Streaming, len(room.Sessions))
	}

	// The transient failure clears; the user retries the share.
	provider.err = nil
	provider.v4 = net.IPv4(203, 0, 113, 5)

	if err := (&StartShare{}).Execute(rooms, hostInfo); err != nil {
		t.Fatalf("retried share returned an unexpected error: %v", err)
	}
	if !host.Streaming {
		t.Error("host is not streaming after a successful retry")
	}
	if got := len(room.Sessions); got != 1 {
		t.Errorf("successful retry created %d sessions, want 1 (one per other room member)", got)
	}
	if _, ok := rooms.connected[host.ID]; !ok {
		t.Error("host should still be connected after a successful retry")
	}
}
