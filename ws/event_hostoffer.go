package ws

import (
	"github.com/screego/server/ws/outgoing"
)

func init() {
	register("hostoffer", func() Event {
		return &HostOffer{}
	})
}

type HostOffer outgoing.P2PMessage

func (e *HostOffer) Execute(rooms *Rooms, current ClientInfo) error {
	return forwardP2P(rooms, current, outgoing.P2PMessage(*e), true, func(m outgoing.P2PMessage) outgoing.Message {
		return outgoing.HostOffer(m)
	})
}
