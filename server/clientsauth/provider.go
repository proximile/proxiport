package clientsauth

import (
	"github.com/proximile/proxiport/share/enums"
	"github.com/proximile/proxiport/share/query"
)

type Provider interface {
	// Get returns client authentication credentials from provider or nil
	Get(id string) (*ClientAuth, error)
	// GetFiltered returns authentication credentials and total count filtered
	GetFiltered(filter *query.ListOptions) ([]*ClientAuth, int, error)
	// Add returns true if the client auth was added and false if it already exists
	Add(client *ClientAuth) (bool, error)
	// Update rewrites the stored credential for an existing id. It is used to
	// migrate a matched legacy plaintext credential to a hash at rest after a
	// successful authentication. Providers that are not writeable return an error.
	Update(client *ClientAuth) error
	// Delete returns client auth by id
	Delete(id string) error
	// IsWriteable returns true if provider is writeable
	IsWriteable() bool
	// Source returns a provider source
	Source() enums.ProviderSource
}
