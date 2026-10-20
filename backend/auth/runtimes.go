package auth

import (
	"errors"

	"github.com/insightlabs38-pixel/vsf/backend/bridge"
)

// BridgeRuntimes vouches for runtime sessions from the bridge's validated bindings. Owners maps a paired client to the caller it is paired with; a client with no owner proves nothing.
type BridgeRuntimes struct {
	Bindings *bridge.Bindings
	Owners   func(client string) (string, bool)
}

func (r BridgeRuntimes) Session(id, client string) (RuntimeSession, error) {
	if r.Bindings == nil || r.Owners == nil {
		return RuntimeSession{}, ErrDenied
	}
	owner, ok := r.Owners(client)
	if !ok {
		return RuntimeSession{}, ErrDenied
	}
	b, err := r.Bindings.Get(id, client)
	if err != nil {
		return RuntimeSession{}, errors.Join(ErrDenied, err)
	}
	return RuntimeSession{ID: b.ID, Client: b.Client, Owner: owner, AppID: b.AppID, Origin: b.Origin, Principal: b.Principal, Runtime: b.Runtime, Expires: b.Expires}, nil
}
