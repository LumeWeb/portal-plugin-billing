package credit

import (
	"context"

	"go.lumeweb.com/portal-plugin-billing/pkg/ledger"
	"go.lumeweb.com/queryutil"
)

// CreditRepositoryWithQuery extends ledger.CreditRepository with query capabilities.
// This interface is used at the service layer to keep pkg/ledger clean of queryutil dependencies.
type CreditRepositoryWithQuery interface {
	ledger.CreditRepository

	// ListCredits retrieves credits with filtering, sorting, and pagination.
	ListCredits(ctx context.Context, filters []queryutil.CrudFilter, sorts []queryutil.Sort, pagination queryutil.Pagination) ([]ledger.Credit, int64, error)
}
