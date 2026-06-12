package ws

import (
	"errors"
	"net"
	"testing"

	"github.com/rs/xid"
	"github.com/screego/server/config"
	"github.com/screego/server/ws/outgoing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeIPProvider struct{}

func (fakeIPProvider) Get() (net.IP, net.IP, error) {
	return net.IPv4(127, 0, 0, 1), nil, nil
}

func joinTestRooms(passphrase string) *Rooms {
	owner := xid.New()
	room := &Room{
		ID:         "room1",
		Passphrase: passphrase,
		Mode:       ConnectionLocal,
		Sessions:   map[xid.ID]*RoomSession{},
		Users: map[xid.ID]*User{
			owner: {ID: owner, Name: "owner", Owner: true, _write: make(chan outgoing.Message, 1)},
		},
	}
	return &Rooms{
		Rooms:     map[string]*Room{room.ID: room},
		connected: map[xid.ID]string{},
		config:    config.Config{TurnIPProvider: fakeIPProvider{}},
	}
}

func TestJoinPassphrase(t *testing.T) {
	tests := []struct {
		name          string
		roomPass      string
		givenPass     string
		authenticated bool
		wantRejected  bool
	}{
		{name: "no passphrase joins freely", roomPass: "", givenPass: ""},
		{name: "correct passphrase joins", roomPass: "s3cret", givenPass: "s3cret"},
		{name: "wrong passphrase rejected", roomPass: "s3cret", givenPass: "nope", wantRejected: true},
		{name: "missing passphrase rejected", roomPass: "s3cret", givenPass: "", wantRejected: true},
		{name: "authenticated bypasses passphrase", roomPass: "s3cret", givenPass: "", authenticated: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rooms := joinTestRooms(tt.roomPass)
			current := ClientInfo{
				ID:            xid.New(),
				Authenticated: tt.authenticated,
				Addr:          net.IPv4(127, 0, 0, 1),
				Write:         make(chan outgoing.Message, 8),
			}

			join := &Join{ID: "room1", UserName: "bob", Password: secret(tt.givenPass)}
			err := join.Execute(rooms, current)

			_, joined := rooms.Rooms["room1"].Users[current.ID]
			if tt.wantRejected {
				require.Error(t, err)
				var ce CloseError
				require.True(t, errors.As(err, &ce), "expected CloseError, got %T", err)
				assert.Equal(t, ClosePassphraseRequired, ce.Code)
				// a rejected join must leave no trace
				assert.Empty(t, rooms.connected[current.ID])
				assert.False(t, joined, "user must not be added on rejection")
			} else {
				require.NoError(t, err)
				assert.Equal(t, "room1", rooms.connected[current.ID])
				assert.True(t, joined, "user should be added on success")
			}
		})
	}
}
