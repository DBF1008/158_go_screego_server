package ws

import (
	"encoding/json"
	"testing"

	"github.com/rs/xid"
	"github.com/screego/server/ws/outgoing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// p2pCase describes one of the four P2P signaling events so the shared
// forwarding behavior can be exercised uniformly across all directions.
type p2pCase struct {
	name     string
	fromHost bool // true when the host is the legitimate sender
	wantType string
	exec     func(sid xid.ID, value json.RawMessage) Event
	cast     func(outgoing.Message) (outgoing.P2PMessage, bool)
}

var p2pCases = []p2pCase{
	{
		name: "hostoffer", fromHost: true, wantType: "hostoffer",
		exec: func(sid xid.ID, v json.RawMessage) Event { return &HostOffer{SID: sid, Value: v} },
		cast: func(m outgoing.Message) (outgoing.P2PMessage, bool) {
			v, ok := m.(outgoing.HostOffer)
			return outgoing.P2PMessage(v), ok
		},
	},
	{
		name: "hostice", fromHost: true, wantType: "hostice",
		exec: func(sid xid.ID, v json.RawMessage) Event { return &HostICE{SID: sid, Value: v} },
		cast: func(m outgoing.Message) (outgoing.P2PMessage, bool) {
			v, ok := m.(outgoing.HostICE)
			return outgoing.P2PMessage(v), ok
		},
	},
	{
		name: "clientice", fromHost: false, wantType: "clientice",
		exec: func(sid xid.ID, v json.RawMessage) Event { return &ClientICE{SID: sid, Value: v} },
		cast: func(m outgoing.Message) (outgoing.P2PMessage, bool) {
			v, ok := m.(outgoing.ClientICE)
			return outgoing.P2PMessage(v), ok
		},
	},
	{
		name: "clientanswer", fromHost: false, wantType: "clientanswer",
		exec: func(sid xid.ID, v json.RawMessage) Event { return &ClientAnswer{SID: sid, Value: v} },
		cast: func(m outgoing.Message) (outgoing.P2PMessage, bool) {
			v, ok := m.(outgoing.ClientAnswer)
			return outgoing.P2PMessage(v), ok
		},
	},
}

// p2pTestRoom builds a room containing a host, a client and an unrelated third
// user, with a single session linking host and client. The returned channels
// are the per-user write channels, so delivery can be asserted directly.
func p2pTestRoom(t *testing.T) (rooms *Rooms, host, client, other ClientInfo, hostCh, clientCh chan outgoing.Message, sid xid.ID) {
	t.Helper()
	hostID, clientID, otherID := xid.New(), xid.New(), xid.New()
	sid = xid.New()
	hostCh = make(chan outgoing.Message, 1)
	clientCh = make(chan outgoing.Message, 1)
	otherCh := make(chan outgoing.Message, 1)

	room := &Room{
		ID:   "room",
		Mode: ConnectionLocal,
		Users: map[xid.ID]*User{
			hostID:   {ID: hostID, _write: hostCh},
			clientID: {ID: clientID, _write: clientCh},
			otherID:  {ID: otherID, _write: otherCh},
		},
		Sessions: map[xid.ID]*RoomSession{
			sid: {Host: hostID, Client: clientID},
		},
	}
	rooms = &Rooms{
		Rooms:     map[string]*Room{"room": room},
		connected: map[xid.ID]string{hostID: "room", clientID: "room", otherID: "room"},
	}
	return rooms, ClientInfo{ID: hostID}, ClientInfo{ID: clientID}, ClientInfo{ID: otherID}, hostCh, clientCh, sid
}

func assertNoMessage(t *testing.T, ch chan outgoing.Message) {
	t.Helper()
	select {
	case m := <-ch:
		t.Fatalf("expected no message, got %#v", m)
	default:
	}
}

// TestP2PForwardDeliversToPeer verifies that each event is forwarded to the
// opposite peer of the session, with its payload and wire type preserved, and
// that the sender does not receive an echo.
func TestP2PForwardDeliversToPeer(t *testing.T) {
	for _, tc := range p2pCases {
		t.Run(tc.name, func(t *testing.T) {
			rooms, host, client, _, hostCh, clientCh, sid := p2pTestRoom(t)

			sender := client
			recipient, bystander := hostCh, clientCh
			if tc.fromHost {
				sender = host
				recipient, bystander = clientCh, hostCh
			}

			value := json.RawMessage(`{"candidate":"x"}`)
			require.NoError(t, tc.exec(sid, value).Execute(rooms, sender))

			select {
			case msg := <-recipient:
				got, ok := tc.cast(msg)
				require.Truef(t, ok, "unexpected outgoing type %T", msg)
				assert.Equal(t, sid, got.SID)
				assert.Equal(t, value, got.Value)
				assert.Equal(t, tc.wantType, msg.Type())
			default:
				t.Fatal("expected a forwarded message to the peer")
			}

			assertNoMessage(t, bystander)
		})
	}
}

// TestP2PUnknownSessionIsIgnored verifies that referencing a session that does
// not exist is tolerated: no error, no delivery, no panic.
func TestP2PUnknownSessionIsIgnored(t *testing.T) {
	for _, tc := range p2pCases {
		t.Run(tc.name, func(t *testing.T) {
			rooms, host, client, _, hostCh, clientCh, _ := p2pTestRoom(t)

			sender := client
			if tc.fromHost {
				sender = host
			}

			require.NoError(t, tc.exec(xid.New(), nil).Execute(rooms, sender))
			assertNoMessage(t, hostCh)
			assertNoMessage(t, clientCh)
		})
	}
}

// TestP2PPermissionDenied verifies that only the legitimate sender for a
// direction may forward: the opposite peer and an unrelated in-room user are
// both rejected, and nothing is delivered.
func TestP2PPermissionDenied(t *testing.T) {
	for _, tc := range p2pCases {
		t.Run(tc.name, func(t *testing.T) {
			rooms, host, client, other, hostCh, clientCh, sid := p2pTestRoom(t)

			wrongPeer := host
			if tc.fromHost {
				wrongPeer = client
			}

			for _, sender := range []ClientInfo{wrongPeer, other} {
				err := tc.exec(sid, nil).Execute(rooms, sender)
				require.Error(t, err)
				assert.Contains(t, err.Error(), "permission denied")
			}

			assertNoMessage(t, hostCh)
			assertNoMessage(t, clientCh)
		})
	}
}

// TestP2PNotConnected verifies the room lookup error is propagated when the
// caller is not connected to any room.
func TestP2PNotConnected(t *testing.T) {
	for _, tc := range p2pCases {
		t.Run(tc.name, func(t *testing.T) {
			rooms, _, _, _, _, _, sid := p2pTestRoom(t)

			stranger := ClientInfo{ID: xid.New()}
			err := tc.exec(sid, nil).Execute(rooms, stranger)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not connected")
		})
	}
}
