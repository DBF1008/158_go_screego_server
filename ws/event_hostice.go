package ws

import (
	"github.com/screego/server/ws/outgoing"
)

func init() {
	register("hostice", func() Event {
		return &HostICE{}
	})
}

type HostICE outgoing.P2PMessage

func (e *HostICE) Execute(rooms *Rooms, current ClientInfo) error {
	return forwardP2P(rooms, current, outgoing.P2PMessage(*e), true, func(m outgoing.P2PMessage) outgoing.Message {
		return outgoing.HostICE(m)
	})
}
