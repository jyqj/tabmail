package app

import (
	"context"
	"fmt"

	"tabmail/internal/hooks"
	"tabmail/internal/models"
	"tabmail/internal/store"
)

// EventEnqueuer is the durable event boundary used by application mutations.
// hooks.Dispatcher implements it by inserting an outbox row with the supplied
// context, so it participates in the same Unit of Work.
type EventEnqueuer interface {
	Enqueue(ctx context.Context, event hooks.Event) error
}

// CommitMutation executes a domain mutation, its audit obligation, and its
// durable integration event in one database transaction. The callback must use
// the context it receives for every repository call; using the outer context is
// intentionally treated as leaving the Unit of Work.
func CommitMutation(
	ctx context.Context,
	tx store.Transactor,
	audits AuditStore,
	events EventEnqueuer,
	audit models.AuditEntry,
	event *hooks.Event,
	mutate func(context.Context) error,
) error {
	return WithinTransaction(ctx, tx, func(txCtx context.Context) error {
		if err := mutate(txCtx); err != nil {
			return err
		}
		if err := InsertAuditRequired(txCtx, audits, audit); err != nil {
			return err
		}
		if event != nil && events != nil {
			if err := events.Enqueue(txCtx, *event); err != nil {
				return fmt.Errorf("enqueue domain event: %w", err)
			}
		}
		return nil
	})
}
