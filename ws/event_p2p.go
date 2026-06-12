package ws

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"github.com/screego/server/ws/outgoing"
)

// forwardP2P validates the sender's session membership and permission, then
// forwards the P2P payload to the peer. All four P2P signaling handlers
// (hostoffer, hostice, clientanswer, clientice) share this exact sequence.
//
// Parameters:
//   - sender:  the authenticated client ID of the message sender
//   - msg:     the raw P2P message (session ID + opaque payload)
//   - isHost:  true when the sender must be the session host (hostoffer/hostice),
//     false when the sender must be the session client (clientanswer/clientice)
//   - wrap:    converts the raw P2P payload into the typed outgoing message
func forwardP2P(rooms *Rooms, sender ClientInfo, msg outgoing.P2PMessage, isHost bool, wrap func(outgoing.P2PMessage) outgoing.Message) error {
	room, err := rooms.CurrentRoom(sender)
	if err != nil {
		return err
	}

	session, ok := room.Sessions[msg.SID]
	if !ok {
		log.Debug().Str("id", msg.SID.String()).Msg("unknown session")
		return nil
	}

	if isHost && session.Host != sender.ID {
		return fmt.Errorf("permission denied for session %s", msg.SID)
	}
	if !isHost && session.Client != sender.ID {
		return fmt.Errorf("permission denied for session %s", msg.SID)
	}

	target := session.Client
	if !isHost {
		target = session.Host
	}

	room.Users[target].WriteTimeout(wrap(msg))

	return nil
}
