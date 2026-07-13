package clientsauth

var SupportedFilters = map[string]bool{
	"id": true,
}

var SupportedSorts = map[string]bool{}

// ClientAuth represents proxiport client authentication credentials.
//
// Password is accepted on write (POST) but never serialized back on read: the
// API redacts it (the server stores a bcrypt hash and cannot reveal the
// original), so it carries "omitempty" and read handlers blank it before
// responding.
type ClientAuth struct {
	ID       string `json:"id" db:"id"`
	Password string `json:"password,omitempty" db:"password"`
}
