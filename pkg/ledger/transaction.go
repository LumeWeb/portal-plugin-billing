package ledger

import (
	"context"
)

// TransactionContext defines the interface for transactional operations.
// Implementations manage transaction boundaries (commit/rollback) atomically.
type TransactionContext interface {
	// Begin begins a new transaction and returns a context with transaction.
	Begin(ctx context.Context) (context.Context, error)

	// Commit commits the current transaction.
	Commit(ctx context.Context) error

	// Rollback aborts the current transaction.
	Rollback(ctx context.Context) error
}
