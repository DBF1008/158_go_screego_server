package ws

import (
	"github.com/gorilla/websocket"
	"github.com/screego/server/ws/outgoing"
)

type Disconnected struct {
	Code   int
	Reason string
}

func (e *Disconnected) Execute(rooms *Rooms, current ClientInfo) error {
	e.executeNoError(rooms, current)
	return nil
}

func (e *Disconnected) executeNoError(rooms *Rooms, current ClientInfo) {
	roomID := rooms.connected[current.ID]
	delete(rooms.connected, current.ID)
	writeTimeout[outgoing.Message](current.Write, outgoing.CloseWriter{Code: e.Code, Reason: e.Reason})

	if roomID == "" {
		return
	}

	room, ok := rooms.Rooms[roomID]
	if !ok {
		// room may already be removed
		return
	}

	user, ok := room.Users[current.ID]

	if !ok {
		// room may already be removed
		return
	}

	delete(room.Users, current.ID)
	usersLeftTotal.Inc()

	room.closeUserSessions(rooms, current.ID, false)

	if user.Owner && room.CloseOnOwnerLeave {
		for _, member := range room.Users {
			delete(rooms.connected, member.ID)
			member.WriteTimeout(outgoing.CloseWriter{Code: websocket.CloseNormalClosure, Reason: CloseOwnerLeft})
		}
		rooms.closeRoom(roomID)
		return
	}

	if len(room.Users) == 0 {
		rooms.closeRoom(roomID)
		return
	}

	room.notifyInfoChanged()
}
