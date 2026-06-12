package ws

import (
	"net"
	"testing"

	"github.com/rs/xid"
	"github.com/screego/server/config"
	"github.com/screego/server/config/ipdns"
	"github.com/screego/server/ws/outgoing"
)

// testRooms builds an in-memory Rooms suitable for exercising event handlers
// without any network or TURN server. Rooms use ConnectionLocal so the TURN
// server is never touched.
func testRooms() *Rooms {
	return &Rooms{
		Rooms:     map[string]*Room{},
		connected: map[xid.ID]string{},
		config: config.Config{
			TurnIPProvider: &ipdns.Static{V4: net.IPv4(1, 2, 3, 4)},
			TurnPort:       "3478",
		},
	}
}

func newTestRoom(rooms *Rooms, id string) *Room {
	room := &Room{
		ID:       id,
		Mode:     ConnectionLocal,
		Users:    map[xid.ID]*User{},
		Sessions: map[xid.ID]*RoomSession{},
	}
	rooms.Rooms[id] = room
	return room
}

// addUser registers a member in the room with a buffered write channel so that
// WriteTimeout never blocks during tests.
func addUser(rooms *Rooms, room *Room, name string) xid.ID {
	id := xid.New()
	room.Users[id] = &User{
		ID:     id,
		Name:   name,
		Addr:   net.IPv4(127, 0, 0, 1),
		_write: make(chan outgoing.Message, 64),
	}
	rooms.connected[id] = room.ID
	return id
}

// clientsOf returns the set of clients receiving a stream from host.
func clientsOf(room *Room, host xid.ID) map[xid.ID]bool {
	out := map[xid.ID]bool{}
	for _, s := range room.Sessions {
		if s.Host == host {
			out[s.Client] = true
		}
	}
	return out
}

func TestStartShareBroadcastsToEveryone(t *testing.T) {
	rooms := testRooms()
	room := newTestRoom(rooms, "room")
	host := addUser(rooms, room, "host")
	a := addUser(rooms, room, "a")
	b := addUser(rooms, room, "b")

	if err := (&StartShare{}).Execute(rooms, ClientInfo{ID: host}); err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	clients := clientsOf(room, host)
	if len(clients) != 2 || !clients[a] || !clients[b] {
		t.Fatalf("expected broadcast to a and b, got %v", clients)
	}
	if mode := room.Users[host].shareMode(); mode != outgoing.ShareEveryone {
		t.Fatalf("expected mode Everyone, got %q", mode)
	}
}

func TestStartShareTargetsSelectedOnly(t *testing.T) {
	rooms := testRooms()
	room := newTestRoom(rooms, "room")
	host := addUser(rooms, room, "host")
	_ = addUser(rooms, room, "a")
	b := addUser(rooms, room, "b")

	if err := (&StartShare{Targets: []xid.ID{b}}).Execute(rooms, ClientInfo{ID: host}); err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	clients := clientsOf(room, host)
	if len(clients) != 1 || !clients[b] {
		t.Fatalf("expected a single session to b, got %v", clients)
	}
	if mode := room.Users[host].shareMode(); mode != outgoing.ShareSelected {
		t.Fatalf("expected mode Selected, got %q", mode)
	}
}

func TestStartShareIgnoresSelfAndUnknownTargets(t *testing.T) {
	rooms := testRooms()
	room := newTestRoom(rooms, "room")
	host := addUser(rooms, room, "host")
	b := addUser(rooms, room, "b")

	// Targets include the sharer itself and a stranger that is not in the room.
	stranger := xid.New()
	if err := (&StartShare{Targets: []xid.ID{host, stranger, b}}).Execute(rooms, ClientInfo{ID: host}); err != nil {
		t.Fatalf("StartShare: %v", err)
	}

	clients := clientsOf(room, host)
	if len(clients) != 1 || !clients[b] {
		t.Fatalf("expected only b after filtering self/unknown, got %v", clients)
	}
}

func TestJoinExcludedFromSelectedButGetsEveryone(t *testing.T) {
	rooms := testRooms()
	room := newTestRoom(rooms, "room")
	host := addUser(rooms, room, "host")
	a := addUser(rooms, room, "a")
	b := addUser(rooms, room, "b")

	// host shares only with b; a broadcasts to everyone.
	if err := (&StartShare{Targets: []xid.ID{b}}).Execute(rooms, ClientInfo{ID: host}); err != nil {
		t.Fatalf("host StartShare: %v", err)
	}
	if err := (&StartShare{}).Execute(rooms, ClientInfo{ID: a}); err != nil {
		t.Fatalf("a StartShare: %v", err)
	}

	// A new member joins after both shares started.
	c := xid.New()
	join := &Join{ID: "room", UserName: "c"}
	if err := join.Execute(rooms, ClientInfo{ID: c, Addr: net.IPv4(127, 0, 0, 1), Write: make(chan outgoing.Message, 64)}); err != nil {
		t.Fatalf("Join: %v", err)
	}

	// c must receive a's broadcast but not host's targeted share.
	var hostsForC []xid.ID
	for _, s := range room.Sessions {
		if s.Client == c {
			hostsForC = append(hostsForC, s.Host)
		}
	}
	if len(hostsForC) != 1 || hostsForC[0] != a {
		t.Fatalf("expected new joiner to receive only from a (Everyone), got hosts %v", hostsForC)
	}
}

func TestStopShareResetsTargeting(t *testing.T) {
	rooms := testRooms()
	room := newTestRoom(rooms, "room")
	host := addUser(rooms, room, "host")
	b := addUser(rooms, room, "b")

	if err := (&StartShare{Targets: []xid.ID{b}}).Execute(rooms, ClientInfo{ID: host}); err != nil {
		t.Fatalf("StartShare: %v", err)
	}
	if len(clientsOf(room, host)) != 1 {
		t.Fatalf("expected one session before stop")
	}

	if err := (&StopShare{}).Execute(rooms, ClientInfo{ID: host}); err != nil {
		t.Fatalf("StopShare: %v", err)
	}

	if got := len(clientsOf(room, host)); got != 0 {
		t.Fatalf("expected all host sessions closed, got %d", got)
	}
	u := room.Users[host]
	if u.Streaming {
		t.Fatalf("expected Streaming=false after stop")
	}
	if u.ShareAllowList != nil {
		t.Fatalf("expected ShareAllowList reset to nil after stop, got %v", u.ShareAllowList)
	}
	if u.shareMode() != outgoing.ShareEveryone {
		t.Fatalf("expected mode back to Everyone after stop, got %q", u.shareMode())
	}
}

func TestUserSharesAndMode(t *testing.T) {
	u := &User{}
	// nil allow-list => broadcast to everyone.
	if !u.shares(xid.New()) {
		t.Fatal("nil allow-list should share with everyone")
	}
	if u.shareMode() != outgoing.ShareEveryone {
		t.Fatalf("nil allow-list mode = %q, want Everyone", u.shareMode())
	}

	target := xid.New()
	u.ShareAllowList = map[xid.ID]bool{target: true}
	if !u.shares(target) {
		t.Fatal("listed target should receive the stream")
	}
	if u.shares(xid.New()) {
		t.Fatal("non-listed member should not receive the stream")
	}
	if u.shareMode() != outgoing.ShareSelected {
		t.Fatalf("non-nil allow-list mode = %q, want Selected", u.shareMode())
	}

	// Empty (non-nil) allow-list => selected, but shares with nobody.
	u.ShareAllowList = map[xid.ID]bool{}
	if u.shares(xid.New()) {
		t.Fatal("empty selected allow-list should share with nobody")
	}
	if u.shareMode() != outgoing.ShareSelected {
		t.Fatalf("empty allow-list mode = %q, want Selected", u.shareMode())
	}
}
