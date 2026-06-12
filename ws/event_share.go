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

	v4, v6, err := rooms.config.TurnIPProvider.Get()
	if err != nil {
		log.Warn().Err(err).
			Str("user", current.ID.String()).
			Str("room", room.ID).
			Msg("failed to resolve TURN addresses, share attempt aborted")
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
