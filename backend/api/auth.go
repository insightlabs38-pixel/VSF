package api

// Authenticator turns a credential presented on one surface into a Principal for that surface only.
type Authenticator interface {
	Authenticate(surface Surface, credential string) (Principal, *Error)
}
