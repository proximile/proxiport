package clientsauth

import (
	"errors"

	"github.com/proximile/proxiport/share/enums"
	"github.com/proximile/proxiport/share/query"
)

type SingleProvider struct {
	client *ClientAuth
}

var _ Provider = &SingleProvider{}

func NewSingleProvider(id, password string) *SingleProvider {
	return &SingleProvider{
		client: &ClientAuth{
			ID:       id,
			Password: password,
		},
	}
}

// GetFiltered returns a copy. A caller that received the live *ClientAuth and
// wrote to it -- as the API handlers once did, to redact the password before
// responding -- would be editing the credential this server authenticates
// agents against, not editing a response.
func (c *SingleProvider) GetFiltered(filter *query.ListOptions) ([]*ClientAuth, int, error) {
	credential := *c.client
	var ca = []*ClientAuth{&credential}
	if len(filter.Filters) > 0 {
		match, err := query.MatchesFilters(ca[0], filter.Filters)
		if err != nil {
			return nil, 0, err
		}
		if match {
			return ca, 1, nil
		}
		return []*ClientAuth{}, 0, nil
	}
	start, _ := filter.Pagination.GetStartEnd(1)
	if start > 0 {
		return []*ClientAuth{}, 1, nil
	}
	return ca, 1, nil
}

// Get returns a copy, for the same reason as GetFiltered.
func (c *SingleProvider) Get(id string) (*ClientAuth, error) {
	if c.client.ID == id {
		credential := *c.client
		return &credential, nil
	}
	return nil, nil
}

func (c *SingleProvider) Add(*ClientAuth) (bool, error) {
	return false, errors.New("not implemented")
}

func (c *SingleProvider) Update(*ClientAuth) error {
	return errors.New("not implemented")
}

func (c *SingleProvider) Delete(string) error {
	return errors.New("not implemented")
}

func (c *SingleProvider) IsWriteable() bool {
	return false
}

func (c *SingleProvider) Source() enums.ProviderSource {
	return enums.ProviderSourceStatic
}
