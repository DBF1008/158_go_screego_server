package ws

import (
	"github.com/rs/xid"
)

func init() {
	register("share", func() Event {
		return &StartShare{}
	})
}

type StartShare struct {
	Mode          ShareMode `json:"mode,omitempty"`
	SelectedUsers []xid.ID  `json:"selectedUsers,omitempty"`
}

func (e *StartShare) Execute(rooms *Rooms, current ClientInfo) error {
	room, err := rooms.CurrentRoom(current)
	if err != nil {
		return err
	}

	user := room.Users[current.ID]
	user.Streaming = true

	if e.Mode == "" {
		e.Mode = ShareModeEveryone
	}
	user.ShareMode = e.Mode

	if e.Mode == ShareModeSelected {
		user.SelectedViewers = make(map[xid.ID]bool)
		for _, id := range e.SelectedUsers {
			if _, exists := room.Users[id]; exists && id != current.ID {
				user.SelectedViewers[id] = true
			}
		}
	} else {
		user.SelectedViewers = nil
	}

	v4, v6, err := rooms.config.TurnIPProvider.Get()
	if err != nil {
		return err
	}

	for _, other := range room.Users {
		if current.ID == other.ID {
			continue
		}
		if !user.isSelected(other.ID) {
			continue
		}
		room.newSession(current.ID, other.ID, rooms, v4, v6)
	}

	room.notifyInfoChanged()
	return nil
}
