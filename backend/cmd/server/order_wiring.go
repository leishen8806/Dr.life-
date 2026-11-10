package main

import (
	"context"

	"github.com/leishen8806/Dr.life-/backend/internal/trade"
)

// unavailableOrderCatalog keeps production fail-closed until the catalog and
// RouteFit reservation contract is connected. HTTP tests use an explicit mock.
type unavailableOrderCatalog struct{}

func (unavailableOrderCatalog) ResolveAndReserve(context.Context, string, []trade.OrderLineInput) ([]trade.CatalogLine, error) {
	return nil, trade.ErrOrderAdaptersUnavailable
}
func (unavailableOrderCatalog) Release(context.Context, string) error {
	return trade.ErrOrderAdaptersUnavailable
}
func (unavailableOrderCatalog) Commit(context.Context, string) error {
	return trade.ErrOrderAdaptersUnavailable
}
