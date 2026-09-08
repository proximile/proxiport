package clientsauth

import "errors"

var SupportedFilters = map[string]bool{
	"id": true,
}

var SupportedSorts = map[string]bool{}

// ClientAuth represents proxiport client authentication credentials.
//
// Password is accepted on write (POST) and never returned on read: the read
// handlers build a separate clientAuthPayload holding only the id.
//
// They used to redact by assigning Password = "" to the object the provider
// returned, which is why the providers now hand out copies -- two of them
// returned a pointer into their own state, so that assignment did not redact a
// response, it blanked the credential the server authenticates agents
// against.
type ClientAuth struct {
	ID       string `json:"id" db:"id"`
	Password string `json:"password,omitempty" db:"password"`
}

// validateStorableCredential refuses to write a credential that would
// authenticate nothing, or everything.
//
// A provider is the last place a blank credential can be stopped before it
// reaches disk, and the paths that write one are not all operator-initiated:
// the at-rest hash upgrade takes whatever the provider currently holds and
// writes it back. An empty password persisted as bcrypt("") is a credential
// that any agent sending an empty password satisfies.
func validateStorableCredential(ca *ClientAuth) error {
	if ca == nil {
		return errors.New("no client auth credential given")
	}
	if ca.ID == "" {
		return errors.New("refusing to store a client auth credential with an empty id")
	}
	if ca.Password == "" {
		return errors.New("refusing to store a client auth credential with an empty password")
	}
	return nil
}
