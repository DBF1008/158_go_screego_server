package ws

import (
	"bytes"

	"github.com/rs/xid"
	"github.com/screego/server/ws/outgoing"
)

func init() {
	register("updateselected", func() Event {
		return &UpdateSelected{}
	})
}

type UpdateSelected struct {
	Add    []xid.ID `json:"add,omitempty"`
	Remove []xid.ID `json:"remove,omitempty"`
}

func (e *UpdateSelected) Execute(rooms *Rooms, current ClientInfo) error {
	room, err := rooms.CurrentRoom(current)
	if err != nil {
		return err
	}

	user := room.Users[current.ID]
	if !user.Streaming {
		return nil
	}
	if user.ShareMode != ShareModeSelected {
		return nil
	}

	v4, v6, err := rooms.config.TurnIPProvider.Get()
	if err != nil {
		return err
	}

	for _, id := range e.Add {
		target, exists := room.Users[id]
		if !exists || id == current.ID {
			continue
		}
		if user.SelectedViewers[id] {
			continue
		}
		user.addSelectedViewer(id)
		room.newSession(current.ID, target.ID, rooms, v4, v6)
	}

	for _, id := range e.Remove {
		if !user.SelectedViewers[id] {
			continue
		}
		user.removeSelectedViewer(id)
		for sid, session := range room.Sessions {
			if bytes.Equal(session.Host.Bytes(), current.ID.Bytes()) && bytes.Equal(session.Client.Bytes(), id.Bytes()) {
				if client, ok := room.Users[session.Client]; ok {
					client.WriteTimeout(outgoing.EndShare(sid))
				}
				room.closeSession(rooms, sid)
				break
			}
		}
	}

	room.notifyInfoChanged()
	return nil
}
