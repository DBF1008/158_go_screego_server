package ws

import (
	"github.com/rs/zerolog/log"
)

func init() {
	register("share", func() Event {
		return &StartShare{}
	})
}

type StartShare struct{}

func (e *StartShare) Execute(rooms *Rooms, current ClientInfo) error {
	room, err := rooms.CurrentRoom(current)
	if err != nil {
		return err
	}

	// Resolve the TURN/STUN addresses before mutating any room state. This call
	// can fail transiently (e.g. the external TURN address is resolved via DNS).
	// Such a failure must only abort this single share attempt: returning an
	// error here would propagate to Rooms.Start and tear down the whole
	// WebSocket, dropping the user from the room and disrupting everyone else.
	// Instead we log it and leave the room untouched, so the user stays
	// connected, is not marked as streaming, and can simply retry.
	v4, v6, err := rooms.config.TurnIPProvider.Get()
	if err != nil {
		log.Warn().Err(err).Str("id", current.ID.String()).Msg("cannot start share, could not resolve turn address")
		return nil
	}

	room.Users[current.ID].Streaming = true

	for _, user := range room.Users {
		if current.ID == user.ID {
			continue
		}
		room.newSession(current.ID, user.ID, rooms, v4, v6)
	}

	room.notifyInfoChanged()
	return nil
}
