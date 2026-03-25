package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/bjl13/open-cognition/internal/models"
	"github.com/bjl13/open-cognition/internal/pg"
)

// DB wraps a connection pool.
type DB struct {
	pool *pg.Pool
}

// New opens a connection pool and verifies connectivity.
func New(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pg.NewPool(ctx, dsn, 4)
	if err != nil {
		return nil, fmt.Errorf("db: connect: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Close releases all pool connections.
func (d *DB) Close() {
	d.pool.Close()
}

// GetSystemState returns the current system mode row.
func (d *DB) GetSystemState(ctx context.Context) (*models.SystemState, error) {
	var mode, changedBy string
	var changedAt time.Time
	err := d.pool.QueryRow(ctx,
		`SELECT mode, changed_by, changed_at FROM system_state WHERE id = 1`,
		&mode, &changedBy, &changedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("db: get system state: %w", err)
	}
	return &models.SystemState{
		Mode:      mode,
		ChangedBy: changedBy,
		ChangedAt: changedAt.UTC().Format(time.RFC3339),
	}, nil
}

// SetSystemMode updates the single system_state row and returns the new state.
func (d *DB) SetSystemMode(ctx context.Context, mode, actor string) (*models.SystemState, error) {
	now := time.Now().UTC()
	sql := fmt.Sprintf(
		`UPDATE system_state SET mode=%s, changed_by=%s, changed_at=%s WHERE id=1`,
		pg.QuoteLiteral(mode),
		pg.QuoteLiteral(actor),
		pg.QuoteLiteral(now.Format(time.RFC3339)),
	)
	if err := d.pool.Exec(ctx, sql); err != nil {
		return nil, fmt.Errorf("db: set system mode: %w", err)
	}
	return &models.SystemState{
		Mode:      mode,
		ChangedBy: actor,
		ChangedAt: now.Format(time.RFC3339),
	}, nil
}

// CanonicalObjectExists reports whether a canonical object with the given ID
// already exists in the ledger.
func (d *DB) CanonicalObjectExists(ctx context.Context, id string) (bool, error) {
	// id is validated upstream to match ^sha256:[0-9a-f]{64}$ (safe characters only),
	// but we quote it anyway for defence in depth.
	sql := fmt.Sprintf(
		`SELECT EXISTS(SELECT 1 FROM canonical_objects WHERE id=%s)`,
		pg.QuoteLiteral(id),
	)
	var exists bool
	if err := d.pool.QueryRow(ctx, sql, &exists); err != nil {
		return false, fmt.Errorf("db: check canonical exists: %w", err)
	}
	return exists, nil
}

// InsertCanonicalObject writes a new canonical object record.
// Caller must verify the object does not already exist.
func (d *DB) InsertCanonicalObject(ctx context.Context, obj *models.CanonicalObject) error {
	createdAt, err := time.Parse(time.RFC3339, obj.CreatedAt)
	if err != nil {
		return fmt.Errorf("db: parse created_at: %w", err)
	}

	var metaJSON []byte
	if obj.Metadata != nil {
		if metaJSON, err = json.Marshal(obj.Metadata); err != nil {
			return fmt.Errorf("db: marshal metadata: %w", err)
		}
	}

	sql := fmt.Sprintf(
		`INSERT INTO canonical_objects
		    (id, object_type, content_type, size_bytes, created_by, created_at, storage_path, metadata)
		 VALUES (%s, %s, %s, %d, %s, %s, %s, %s)`,
		pg.QuoteLiteral(obj.ID),
		pg.QuoteLiteral(obj.ObjectType),
		pg.QuoteLiteral(obj.ContentType),
		obj.SizeBytes,
		pg.QuoteLiteral(obj.CreatedBy),
		pg.QuoteLiteral(createdAt.UTC().Format(time.RFC3339)),
		pg.QuoteLiteral(obj.StoragePath),
		pg.FormatJSONOrNULL(metaJSON),
	)
	if err := d.pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("db: insert canonical object: %w", err)
	}
	return nil
}

// InsertAgentReference writes a new agent reference record.
// Caller must verify canonical_object_id exists.
func (d *DB) InsertAgentReference(ctx context.Context, ref *models.AgentReference) error {
	createdAt, err := time.Parse(time.RFC3339, ref.CreatedAt)
	if err != nil {
		return fmt.Errorf("db: parse created_at: %w", err)
	}

	var metaJSON []byte
	if ref.Metadata != nil {
		if metaJSON, err = json.Marshal(ref.Metadata); err != nil {
			return fmt.Errorf("db: marshal metadata: %w", err)
		}
	}

	timeHorizon := pg.QuoteLiteralOrNULL(nullIfEmpty(ref.TimeHorizon))
	signature := pg.QuoteLiteralOrNULL(nullIfEmpty(ref.Signature))

	sql := fmt.Sprintf(
		`INSERT INTO agent_references
		    (id, canonical_object_id, agent_id, created_at, context,
		     relevance, trust_weight, time_horizon, signature, metadata)
		 VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s)`,
		pg.QuoteLiteral(ref.ID),
		pg.QuoteLiteral(ref.CanonicalObjectID),
		pg.QuoteLiteral(ref.AgentID),
		pg.QuoteLiteral(createdAt.UTC().Format(time.RFC3339)),
		pg.QuoteLiteral(ref.Context),
		pg.FormatFloat(ref.Relevance),
		pg.FormatFloat(ref.TrustWeight),
		timeHorizon,
		signature,
		pg.FormatJSONOrNULL(metaJSON),
	)
	if err := d.pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("db: insert agent reference: %w", err)
	}
	return nil
}

// WriteAuditLog appends an event to the audit log.
// targetID and targetType may be empty for lifecycle events.
func (d *DB) WriteAuditLog(ctx context.Context, actor, action, targetID, targetType string, detail map[string]interface{}) error {
	var detailJSON []byte
	var err error
	if detail != nil {
		if detailJSON, err = json.Marshal(detail); err != nil {
			return fmt.Errorf("db: marshal audit detail: %w", err)
		}
	}

	sql := fmt.Sprintf(
		`INSERT INTO audit_log (actor, action, target_id, target_type, detail)
		 VALUES (%s, %s, %s, %s, %s)`,
		pg.QuoteLiteral(actor),
		pg.QuoteLiteral(action),
		pg.QuoteLiteralOrNULL(nullIfEmpty(targetID)),
		pg.QuoteLiteralOrNULL(nullIfEmpty(targetType)),
		pg.FormatJSONOrNULL(detailJSON),
	)
	if err := d.pool.Exec(ctx, sql); err != nil {
		return fmt.Errorf("db: write audit log: %w", err)
	}
	return nil
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
