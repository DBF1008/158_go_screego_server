package ws

import (
	"fmt"
	"net"
	"sort"
	"time"

	"github.com/rs/xid"
	"github.com/rs/zerolog/log"
	"github.com/screego/server/config"
	"github.com/screego/server/ws/outgoing"
)

type ConnectionMode string

const (
	ConnectionLocal ConnectionMode = "local"
	ConnectionSTUN  ConnectionMode = "stun"
	ConnectionTURN  ConnectionMode = config.AuthModeTurn
)

type Room struct {
	ID                string
	CloseOnOwnerLeave bool
	Mode              ConnectionMode
	Users             map[xid.ID]*User
	Sessions          map[xid.ID]*RoomSession
}

const (
	CloseOwnerLeft = "Owner Left"
	CloseDone      = "Read End"
)

func (r *Room) newSession(host, client xid.ID, rooms *Rooms, v4, v6 net.IP) {
	id := xid.New()
	r.Sessions[id] = &RoomSession{
		Host:   host,
		Client: client,
	}
	sessionCreatedTotal.Inc()

	iceHost := []outgoing.ICEServer{}
	iceClient := []outgoing.ICEServer{}
	switch r.Mode {
	case ConnectionLocal:
	case ConnectionSTUN:
		iceHost = []outgoing.ICEServer{{URLs: rooms.addresses("stun", v4, v6, false)}}
		iceClient = []outgoing.ICEServer{{URLs: rooms.addresses("stun", v4, v6, false)}}
	case ConnectionTURN:
		hostName, hostPW := rooms.turnServer.Credentials(id.String()+"host", r.Users[host].Addr)
		clientName, clientPW := rooms.turnServer.Credentials(id.String()+"client", r.Users[client].Addr)
		iceHost = []outgoing.ICEServer{{
			URLs:       rooms.addresses("turn", v4, v6, true),
			Credential: hostPW,
			Username:   hostName,
		}}
		iceClient = []outgoing.ICEServer{{
			URLs:       rooms.addresses("turn", v4, v6, true),
			Credential: clientPW,
			Username:   clientName,
		}}
	}
	r.Users[host].WriteTimeout(outgoing.HostSession{Peer: client, ID: id, ICEServers: iceHost})
	r.Users[client].WriteTimeout(outgoing.ClientSession{Peer: host, ID: id, ICEServers: iceClient})
}

func (r *Rooms) addresses(prefix string, v4, v6 net.IP, tcp bool) (result []string) {
	if v4 != nil {
		result = append(result, fmt.Sprintf("%s:%s:%s", prefix, v4.String(), r.config.TurnPort))
		if tcp {
			result = append(result, fmt.Sprintf("%s:%s:%s?transport=tcp", prefix, v4.String(), r.config.TurnPort))
		}
	}
	if v6 != nil {
		result = append(result, fmt.Sprintf("%s:[%s]:%s", prefix, v6.String(), r.config.TurnPort))
		if tcp {
			result = append(result, fmt.Sprintf("%s:[%s]:%s?transport=tcp", prefix, v6.String(), r.config.TurnPort))
		}
	}
	return
}

func (r *Room) closeSession(rooms *Rooms, id xid.ID) {
	if r.Mode == ConnectionTURN {
		rooms.turnServer.Disallow(id.String() + "host")
		rooms.turnServer.Disallow(id.String() + "client")
	}
	delete(r.Sessions, id)
	sessionClosedTotal.Inc()
}

// closeUserSessions tears down the sessions a user takes part in and notifies the
// surviving peer of each closed session with EndShare. When hostOnly is true only
// the sessions the user streams (is host of) are closed, which is used when a user
// stops sharing but stays in the room; otherwise every session involving the user
// is closed, used when the user leaves the room entirely.
//
// This is the single entry point for "a participant leaves a session and the peer
// must be told". It funnels through closeSession, the one place that releases TURN
// credentials and records the session-closed metric, so notification, metrics and
// credential release stay consistent across the different exit scenarios.
func (r *Room) closeUserSessions(rooms *Rooms, user xid.ID, hostOnly bool) {
	for id, session := range r.Sessions {
		if hostOnly && session.Host != user {
			continue
		}
		peerID, ok := session.peer(user)
		if !ok {
			continue
		}
		if peerUser, ok := r.Users[peerID]; ok {
			peerUser.WriteTimeout(outgoing.EndShare(id))
		}
		r.closeSession(rooms, id)
	}
}

type RoomSession struct {
	Host   xid.ID
	Client xid.ID
}

// peer returns the other participant of the session relative to user and reports
// whether user actually takes part in this session.
func (s *RoomSession) peer(user xid.ID) (xid.ID, bool) {
	switch user {
	case s.Host:
		return s.Client, true
	case s.Client:
		return s.Host, true
	default:
		return xid.ID{}, false
	}
}

func (r *Room) notifyInfoChanged() {
	for _, current := range r.Users {
		users := []outgoing.User{}
		for _, user := range r.Users {
			users = append(users, outgoing.User{
				ID:        user.ID,
				Name:      user.Name,
				Streaming: user.Streaming,
				You:       current == user,
				Owner:     user.Owner,
			})
		}

		sort.Slice(users, func(i, j int) bool {
			left := users[i]
			right := users[j]

			if left.Owner != right.Owner {
				return left.Owner
			}

			if left.Streaming != right.Streaming {
				return left.Streaming
			}

			return left.Name < right.Name
		})

		current.WriteTimeout(outgoing.Room{
			ID:    r.ID,
			Users: users,
		})
	}
}

type User struct {
	ID        xid.ID
	Addr      net.IP
	Name      string
	Streaming bool
	Owner     bool
	_write    chan<- outgoing.Message
}

func (u *User) WriteTimeout(msg outgoing.Message) {
	writeTimeout(u._write, msg)
}

func writeTimeout[T any](ch chan<- T, msg T) {
	select {
	case <-time.After(2 * time.Second):
		log.Warn().Interface("event", fmt.Sprintf("%T", msg)).Interface("payload", msg).Msg("Client write loop didn't accept the message.")
	case ch <- msg:
	}
}
