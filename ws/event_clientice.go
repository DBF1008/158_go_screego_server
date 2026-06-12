package ws

import (
	"github.com/screego/server/ws/outgoing"
)

func init() {
	register("clientice", func() Event {
		return &ClientICE{}
	})
}

type ClientICE outgoing.P2PMessage

func (e *ClientICE) Execute(rooms *Rooms, current ClientInfo) error {
	return forwardP2P(rooms, current, outgoing.P2PMessage(*e), false, func(m outgoing.P2PMessage) outgoing.Message {
		return outgoing.ClientICE(m)
	})
}
