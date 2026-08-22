// Package store is the data access layer over the pavedway Postgres
// schema: sqlc-generated queries plus hand-written bootstrap and helpers.
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
