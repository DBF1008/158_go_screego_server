package ws

// ClosePassphraseRequired is a WebSocket close code (in the private-use 4xxx range,
// which browsers deliver to JavaScript) signalling that a room is passphrase
// protected and the supplied passphrase was missing or incorrect. The frontend
// uses it to show a passphrase prompt instead of a generic error.
const ClosePassphraseRequired = 4001

// CloseError lets an event handler request a specific WebSocket close code instead
// of the default normal closure used for generic errors.
type CloseError struct {
	Code   int
	Reason string
}

func (e CloseError) Error() string {
	return e.Reason
}

// secret is a string that never reveals itself when logged or JSON-marshalled, so
// room passphrases don't leak into the debug payload logs. The underlying value is
// still readable in code via a plain string(...) conversion. It only implements
// json.Marshaler, so incoming payloads still unmarshal into it normally.
type secret string

func (secret) String() string {
	return "<redacted>"
}

func (secret) MarshalJSON() ([]byte, error) {
	return []byte(`"<redacted>"`), nil
}
