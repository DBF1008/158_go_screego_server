package ws

import "github.com/rs/xid"

func init() {
	register("share", func() Event {
		return &StartShare{}
	})
}

type StartShare struct {
	// Targets optionally restricts the share to specific members. When empty or
	// absent the stream is broadcast to everyone in the room (the default).
	Targets []xid.ID `json:"targets,omitempty"`
}

func (e *StartShare) Execute(rooms *Rooms, current ClientInfo) error {
	room, err := rooms.CurrentRoom(current)
	if err != nil {
		return err
	}

	user := room.Users[current.ID]
	user.Streaming = true
	user.ShareAllowList = e.allowList(current.ID, room)

	v4, v6, err := rooms.config.TurnIPProvider.Get()
	if err != nil {
		return err
	}

	for _, member := range room.Users {
		if current.ID == member.ID || !user.shares(member.ID) {
			continue
		}
		room.newSession(current.ID, member.ID, rooms, v4, v6)
	}

	room.notifyInfoChanged()
	return nil
}

// allowList builds the set of members a targeted share is restricted to. It
// returns nil when no targets are given, signalling a broadcast to everyone.
// The sharer's own id and ids not currently in the room are ignored.
func (e *StartShare) allowList(self xid.ID, room *Room) map[xid.ID]bool {
	if len(e.Targets) == 0 {
		return nil
	}
	allow := map[xid.ID]bool{}
	for _, id := range e.Targets {
		if id == self {
			continue
		}
		if _, ok := room.Users[id]; ok {
			allow[id] = true
		}
	}
	return allow
}
