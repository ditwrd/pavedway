package store

import (
	"context"
	"errors"
)

var ErrOrganizationExists = errors.New("organization already exists")

func (q *Queries) BootstrapOrganization(ctx context.Context, name string) (Organization, error) {
	count, err := q.CountOrganizations(ctx)
	if err != nil {
		return Organization{}, err
	}
	if count > 0 {
		return Organization{}, ErrOrganizationExists
	}
	return q.CreateOrganization(ctx, name)
}
