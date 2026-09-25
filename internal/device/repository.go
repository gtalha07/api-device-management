package device

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresRepository(pool *pgxpool.Pool) *PostgresRepository {
	return &PostgresRepository{pool: pool}
}

// Compile-time check whether PostgresRepository satisfies Repository
var _ Repository = (*PostgresRepository)(nil)

const (
	deviceColumns   = `id, name, brand, state, created_at`
	getDeviceQuery  = `SELECT ` + deviceColumns + ` FROM devices WHERE id = $1`
	lockDeviceQuery = getDeviceQuery + ` FOR UPDATE`
)

func (r *PostgresRepository) Create(ctx context.Context, d Device) (Device, error) {
	const query = `
		INSERT INTO devices (name, brand, state)
		VALUES ($1, $2, $3)
		RETURNING id, created_at`

	err := r.pool.QueryRow(ctx, query, d.Name, d.Brand, string(d.State)).
		Scan(&d.ID, &d.CreatedAt)
	if err != nil {
		return Device{}, fmt.Errorf("insert device: %w", err)
	}

	d.CreatedAt = d.CreatedAt.UTC()
	return d, nil
}

func (r *PostgresRepository) Get(ctx context.Context, id string) (Device, error) {
	d, err := scanDevice(r.pool.QueryRow(ctx, getDeviceQuery, id))
	if err != nil {
		return Device{}, fmt.Errorf("get device: %w", err)
	}

	return d, nil
}

func (r *PostgresRepository) List(ctx context.Context, f Filter) ([]Device, error) {
	var (
		conds []string
		args  []any
	)
	if f.Brand != "" {
		args = append(args, f.Brand)
		conds = append(conds, fmt.Sprintf("brand = $%d", len(args)))
	}
	if f.State != "" {
		args = append(args, string(f.State))
		conds = append(conds, fmt.Sprintf("state = $%d", len(args)))
	}

	query := `SELECT ` + deviceColumns + ` FROM devices`
	if len(conds) > 0 {
		query += ` WHERE ` + strings.Join(conds, ` AND `)
	}
	query += ` ORDER BY created_at, id`

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	devices := []Device{}
	for rows.Next() {
		d, err := scanDevice(rows)
		if err != nil {
			return nil, fmt.Errorf("scan device: %w", err)
		}
		devices = append(devices, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}

	return devices, nil
}

func (r *PostgresRepository) Update(ctx context.Context, id string, fn func(d *Device) error) (Device, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return Device{}, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // no-op once Commit has succeeded

	d, err := scanDevice(tx.QueryRow(ctx, lockDeviceQuery, id))
	if err != nil {
		return Device{}, fmt.Errorf("lock device: %w", err)
	}
	if err := fn(&d); err != nil {
		return Device{}, err
	}

	const updateQuery = `
		UPDATE devices SET name = $2, brand = $3, state = $4
		WHERE id = $1
		RETURNING ` + deviceColumns

	updated, err := scanDevice(tx.QueryRow(ctx, updateQuery, id, d.Name, d.Brand, string(d.State)))
	if err != nil {
		return Device{}, fmt.Errorf("update device: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return Device{}, fmt.Errorf("commit: %w", err)
	}

	return updated, nil
}

func (r *PostgresRepository) Delete(ctx context.Context, id string, check func(d Device) error) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	d, err := scanDevice(tx.QueryRow(ctx, lockDeviceQuery, id))
	if err != nil {
		return fmt.Errorf("lock device: %w", err)
	}

	if err := check(d); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `DELETE FROM devices WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete device: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// scanDevice reads one row in deviceColumns order. pgx.Row is satisfied by
// both QueryRow results and pgx.Rows, so Get, List, Update and Delete share it.
func scanDevice(row pgx.Row) (Device, error) {
	var d Device
	err := row.Scan(&d.ID, &d.Name, &d.Brand, &d.State, &d.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Device{}, ErrNotFound
	}
	if err != nil {
		return Device{}, err
	}

	d.CreatedAt = d.CreatedAt.UTC()
	return d, nil
}
