// Package repository defines persistence ports at the domain boundary.
package repository

import "context"

type TransactionManager interface {
	WithinTransaction(ctx context.Context, fn func(context.Context) error) error
}
