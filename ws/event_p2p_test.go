package ws

import (
	"testing"

	"github.com/rs/xid"
	"github.com/screego/server/ws/outgoing"
)

// p2pFixture holds everything needed to exercise forwardP2P in isolation.
type p2pFixture struct {
	rooms       *Rooms
	room        *Room
	hostInfo    ClientInfo
	clientInfo  ClientInfo
	hostWrite   chan outgoing.Message // read side of host's write channel
	clientWrite chan outgoing.Message // read side of client's write channel
}

func newP2PFixture() *p2pFixture {
	hostID := xid.New()
	clientID := xid.New()
	sid := xid.New()

	hostWrite := make(chan outgoing.Message, 4)
	clientWrite := make(chan outgoing.Message, 4)

	room := &Room{
		ID: "test-room",
		Users: map[xid.ID]*User{
			hostID:   {ID: hostID, _write: hostWrite},
			clientID: {ID: clientID, _write: clientWrite},
		},
		Sessions: map[xid.ID]*RoomSession{
			sid: {Host: hostID, Client: clientID},
		},
	}

	rooms := &Rooms{
		Rooms:     map[string]*Room{"test-room": room},
		connected: map[xid.ID]string{hostID: "test-room", clientID: "test-room"},
	}

	return &p2pFixture{
		rooms:       rooms,
		room:        room,
		hostInfo:    ClientInfo{ID: hostID},
		clientInfo:  ClientInfo{ID: clientID},
		hostWrite:   hostWrite,
		clientWrite: clientWrite,
	}
}

func (f *p2pFixture) sessionID() xid.ID {
	for id := range f.room.Sessions {
		return id
	}
	panic("no sessions")
}

func dummyWrap(m outgoing.P2PMessage) outgoing.Message {
	return outgoing.HostOffer(m)
}

// ---- happy paths ----

func TestForwardP2P_HostToClient(t *testing.T) {
	f := newP2PFixture()
	err := forwardP2P(f.rooms, f.hostInfo, outgoing.P2PMessage{SID: f.sessionID()}, true, dummyWrap)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestForwardP2P_ClientToHost(t *testing.T) {
	f := newP2PFixture()
	err := forwardP2P(f.rooms, f.clientInfo, outgoing.P2PMessage{SID: f.sessionID()}, false, dummyWrap)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

// ---- unknown session: silent nil ----

func TestForwardP2P_UnknownSession(t *testing.T) {
	f := newP2PFixture()
	err := forwardP2P(f.rooms, f.hostInfo, outgoing.P2PMessage{SID: xid.New()}, true, dummyWrap)
	if err != nil {
		t.Fatalf("unknown session should return nil, got %v", err)
	}
}

// ---- permission denied ----

func TestForwardP2P_HostSender_AsClient(t *testing.T) {
	f := newP2PFixture()
	// host sends but isHost=false → permission denied
	err := forwardP2P(f.rooms, f.hostInfo, outgoing.P2PMessage{SID: f.sessionID()}, false, dummyWrap)
	if err == nil {
		t.Fatal("expected permission denied error")
	}
}

func TestForwardP2P_ClientSender_AsHost(t *testing.T) {
	f := newP2PFixture()
	// client sends but isHost=true → permission denied
	err := forwardP2P(f.rooms, f.clientInfo, outgoing.P2PMessage{SID: f.sessionID()}, true, dummyWrap)
	if err == nil {
		t.Fatal("expected permission denied error")
	}
}

// ---- not connected / not in a room ----

func TestForwardP2P_NotConnected(t *testing.T) {
	rooms := &Rooms{
		Rooms:     map[string]*Room{},
		connected: map[xid.ID]string{},
	}
	stranger := ClientInfo{ID: xid.New()}
	err := forwardP2P(rooms, stranger, outgoing.P2PMessage{SID: xid.New()}, true, dummyWrap)
	if err == nil {
		t.Fatal("expected error for unconnected sender")
	}
}

func TestForwardP2P_NotInRoom(t *testing.T) {
	uid := xid.New()
	rooms := &Rooms{
		Rooms:     map[string]*Room{},
		connected: map[xid.ID]string{uid: ""},
	}
	err := forwardP2P(rooms, ClientInfo{ID: uid}, outgoing.P2PMessage{SID: xid.New()}, true, dummyWrap)
	if err == nil {
		t.Fatal("expected error for sender not in a room")
	}
}

// ---- forwarding target correctness ----

func TestForwardP2P_HostToClient_ReachesClient(t *testing.T) {
	f := newP2PFixture()
	_ = forwardP2P(f.rooms, f.hostInfo, outgoing.P2PMessage{SID: f.sessionID()}, true, dummyWrap)

	select {
	case msg := <-f.clientWrite:
		if _, ok := msg.(outgoing.HostOffer); !ok {
			t.Fatalf("expected outgoing.HostOffer, got %T", msg)
		}
	default:
		t.Fatal("expected message on client write channel")
	}
}

func TestForwardP2P_ClientToHost_ReachesHost(t *testing.T) {
	f := newP2PFixture()
	wrap := func(m outgoing.P2PMessage) outgoing.Message { return outgoing.ClientAnswer(m) }
	_ = forwardP2P(f.rooms, f.clientInfo, outgoing.P2PMessage{SID: f.sessionID()}, false, wrap)

	select {
	case msg := <-f.hostWrite:
		if _, ok := msg.(outgoing.ClientAnswer); !ok {
			t.Fatalf("expected outgoing.ClientAnswer, got %T", msg)
		}
	default:
		t.Fatal("expected message on host write channel")
	}
}

// ---- host→host must NOT deliver to client ----

func TestForwardP2P_HostToClient_DoesNotReachHost(t *testing.T) {
	f := newP2PFixture()
	_ = forwardP2P(f.rooms, f.hostInfo, outgoing.P2PMessage{SID: f.sessionID()}, true, dummyWrap)

	select {
	case <-f.hostWrite:
		t.Fatal("host should not receive its own forwarded message")
	default:
		// ok
	}
}

// ---- client→host must NOT deliver to client ----

func TestForwardP2P_ClientToHost_DoesNotReachClient(t *testing.T) {
	f := newP2PFixture()
	wrap := func(m outgoing.P2PMessage) outgoing.Message { return outgoing.ClientICE(m) }
	_ = forwardP2P(f.rooms, f.clientInfo, outgoing.P2PMessage{SID: f.sessionID()}, false, wrap)

	select {
	case <-f.clientWrite:
		t.Fatal("client should not receive its own forwarded message")
	default:
		// ok
	}
}
