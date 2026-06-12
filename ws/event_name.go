package ws

func init() {
	register("name", func() Event {
		return &Name{}
	})
}

type Name struct {
	UserName string `json:"username"`
}

func (e *Name) Execute(rooms *Rooms, current ClientInfo) error {
	room, err := rooms.CurrentRoom(current)
	if err != nil {
		return err
	}

	// Authenticated users have their display name bound to their login
	// identity (set on join/create). They must not be able to rename
	// themselves via a name event, otherwise the room could show a name
	// that does not match who they actually are. Guests may still rename.
	if current.Authenticated {
		return nil
	}

	room.Users[current.ID].Name = e.UserName

	room.notifyInfoChanged()
	return nil
}
