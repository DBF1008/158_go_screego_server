package ws

import (
	"fmt"

	"github.com/rs/xid"
	"github.com/rs/zerolog/log"
	"github.com/screego/server/ws/outgoing"
)

func init() {
	register("hostoffer", func() Event { return &HostOffer{} })
	register("hostice", func() Event { return &HostICE{} })
	register("clientice", func() Event { return &ClientICE{} })
	register("clientanswer", func() Event { return &ClientAnswer{} })
}

type HostOffer outgoing.P2PMessage

func (e *HostOffer) Execute(rooms *Rooms, current ClientInfo) error {
	return rooms.forwardP2P(current, e.SID, hostToClient, outgoing.HostOffer(*e))
}

type HostICE outgoing.P2PMessage

func (e *HostICE) Execute(rooms *Rooms, current ClientInfo) error {
	return rooms.forwardP2P(current, e.SID, hostToClient, outgoing.HostICE(*e))
}

type ClientICE outgoing.P2PMessage

func (e *ClientICE) Execute(rooms *Rooms, current ClientInfo) error {
	return rooms.forwardP2P(current, e.SID, clientToHost, outgoing.ClientICE(*e))
}

type ClientAnswer outgoing.P2PMessage

func (e *ClientAnswer) Execute(rooms *Rooms, current ClientInfo) error {
	return rooms.forwardP2P(current, e.SID, clientToHost, outgoing.ClientAnswer(*e))
}

// p2pDirection describes which peer of a session is the legitimate sender of a
// signaling message and, consequently, which peer must receive it.
type p2pDirection bool

const (
	// hostToClient: the host sends, the client receives (offer, host ICE).
	hostToClient p2pDirection = true
	// clientToHost: the client sends, the host receives (answer, client ICE).
	clientToHost p2pDirection = false
)

func (d p2pDirection) senderRecipient(s *RoomSession) (sender, recipient xid.ID) {
	if d == hostToClient {
		return s.Host, s.Client
	}
	return s.Client, s.Host
}

// forwardP2P relays a peer-to-peer signaling message to the other side of the
// referenced session. It is the single place that performs the room lookup,
// session lookup, permission check and delivery shared by every P2P signaling
// event, so that the behavior of all directions stays in lockstep.
//
// Fault-tolerance semantics (identical for every direction):
//   - if the caller is not in a valid room, the room error is returned;
//   - an unknown session is tolerated: it is logged at debug level and ignored
//     (no error), since a peer may legitimately reference a session that has
//     already been torn down;
//   - only the expected sender for the direction may forward; any other caller
//     receives a "permission denied" error.
func (rooms *Rooms) forwardP2P(current ClientInfo, sid xid.ID, dir p2pDirection, msg outgoing.Message) error {
	room, err := rooms.CurrentRoom(current)
	if err != nil {
		return err
	}

	session, ok := room.Sessions[sid]
	if !ok {
		log.Debug().Str("id", sid.String()).Msg("unknown session")
		return nil
	}

	sender, recipient := dir.senderRecipient(session)
	if sender != current.ID {
		return fmt.Errorf("permission denied for session %s", sid)
	}

	room.Users[recipient].WriteTimeout(msg)
	return nil
}
