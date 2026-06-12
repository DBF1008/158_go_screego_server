package ws

import (
	"github.com/screego/server/ws/outgoing"
)

func init() {
	register("clientanswer", func() Event {
		return &ClientAnswer{}
	})
}

type ClientAnswer outgoing.P2PMessage

func (e *ClientAnswer) Execute(rooms *Rooms, current ClientInfo) error {
	return forwardP2P(rooms, current, outgoing.P2PMessage(*e), false, func(m outgoing.P2PMessage) outgoing.Message {
		return outgoing.ClientAnswer(m)
	})
}
