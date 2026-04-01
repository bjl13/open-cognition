package db

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
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

// LookupOrRegisterAgentKey implements trust-on-first-use (TOFU) key registration.
//
// If no key exists for agentID, inserts pubKeyB64 with firstRefID and returns the
// submitted key as storedKey with isNew=true.
//
// If a key already exists, returns the stored key with isNew=false. The caller
// compares storedKey == submittedKey to detect key mismatch.
//
// An audit log entry with action "register_key" is written when isNew is true.
func (d *DB) LookupOrRegisterAgentKey(ctx context.Context, agentID, pubKeyB64, firstRefID string) (storedKey string, isNew bool, err error) {
	insertSQL := fmt.Sprintf(
		`INSERT INTO agent_keys (agent_id, public_key, first_ref_id) VALUES (%s, %s, %s)`,
		pg.QuoteLiteral(agentID),
		pg.QuoteLiteral(pubKeyB64),
		pg.QuoteLiteral(firstRefID),
	)
	insertErr := d.pool.Exec(ctx, insertSQL)
	if insertErr == nil {
		// Newly registered.
		_ = d.WriteAuditLog(ctx, agentID, "register_key", firstRefID, "agent_keys",
			map[string]interface{}{"public_key": pubKeyB64})
		return pubKeyB64, true, nil
	}

	// If it's not a unique-constraint violation, surface the error.
	if !strings.Contains(insertErr.Error(), "duplicate key") {
		return "", false, fmt.Errorf("db: register agent key: %w", insertErr)
	}

	// Key already registered — read it back.
	selectSQL := fmt.Sprintf(
		`SELECT public_key FROM agent_keys WHERE agent_id = %s`,
		pg.QuoteLiteral(agentID),
	)
	var stored string
	if err := d.pool.QueryRow(ctx, selectSQL, &stored); err != nil {
		return "", false, fmt.Errorf("db: lookup agent key: %w", err)
	}
	return stored, false, nil
}

// ListCanonicalObjects returns up to limit canonical objects ordered newest first.
func (d *DB) ListCanonicalObjects(ctx context.Context, limit, offset int) ([]models.CanonicalObject, error) {
	sql := fmt.Sprintf(
		`SELECT id, object_type, content_type, size_bytes, created_by, created_at, storage_path,
		        COALESCE(metadata::text, '')
		 FROM canonical_objects ORDER BY created_at DESC LIMIT %d OFFSET %d`,
		limit, offset,
	)
	rows, err := d.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("db: list canonical objects: %w", err)
	}
	out := make([]models.CanonicalObject, 0, len(rows))
	for _, row := range rows {
		if len(row) < 8 {
			continue
		}
		size, _ := strconv.Atoi(row[3])
		obj := models.CanonicalObject{
			SchemaVersion: "0.1.0",
			ID:            row[0],
			ObjectType:    row[1],
			ContentType:   row[2],
			SizeBytes:     size,
			CreatedBy:     row[4],
			CreatedAt:     parseTimestamp(row[5]),
			StoragePath:   row[6],
		}
		if row[7] != "" {
			var meta map[string]interface{}
			if json.Unmarshal([]byte(row[7]), &meta) == nil {
				obj.Metadata = meta
			}
		}
		out = append(out, obj)
	}
	return out, nil
}

// ListAgentReferences returns up to limit agent references ordered newest first.
func (d *DB) ListAgentReferences(ctx context.Context, limit, offset int) ([]models.AgentReference, error) {
	sql := fmt.Sprintf(
		`SELECT id, canonical_object_id, agent_id, created_at, context,
		        COALESCE(relevance::text, ''), COALESCE(trust_weight::text, ''),
		        COALESCE(time_horizon, ''), COALESCE(signature, ''),
		        COALESCE(metadata::text, '')
		 FROM agent_references ORDER BY created_at DESC LIMIT %d OFFSET %d`,
		limit, offset,
	)
	rows, err := d.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("db: list agent references: %w", err)
	}
	out := make([]models.AgentReference, 0, len(rows))
	for _, row := range rows {
		if len(row) < 10 {
			continue
		}
		ref := models.AgentReference{
			SchemaVersion:     "0.1.0",
			ID:                row[0],
			CanonicalObjectID: row[1],
			AgentID:           row[2],
			CreatedAt:         parseTimestamp(row[3]),
			Context:           row[4],
			TimeHorizon:       row[7],
			Signature:         row[8],
		}
		if row[5] != "" {
			if f, err := strconv.ParseFloat(row[5], 64); err == nil {
				ref.Relevance = &f
			}
		}
		if row[6] != "" {
			if f, err := strconv.ParseFloat(row[6], 64); err == nil {
				ref.TrustWeight = &f
			}
		}
		if row[9] != "" {
			var meta map[string]interface{}
			if json.Unmarshal([]byte(row[9]), &meta) == nil {
				ref.Metadata = meta
			}
		}
		out = append(out, ref)
	}
	return out, nil
}

// ListAuditLog returns up to limit audit entries ordered newest first.
func (d *DB) ListAuditLog(ctx context.Context, limit int) ([]models.AuditEntry, error) {
	sql := fmt.Sprintf(
		`SELECT id, occurred_at, actor, action,
		        COALESCE(target_id, ''), COALESCE(target_type, ''),
		        COALESCE(detail::text, '')
		 FROM audit_log ORDER BY occurred_at DESC LIMIT %d`,
		limit,
	)
	rows, err := d.pool.Query(ctx, sql)
	if err != nil {
		return nil, fmt.Errorf("db: list audit log: %w", err)
	}
	out := make([]models.AuditEntry, 0, len(rows))
	for _, row := range rows {
		if len(row) < 7 {
			continue
		}
		id, _ := strconv.ParseInt(row[0], 10, 64)
		entry := models.AuditEntry{
			ID:         id,
			OccurredAt: parseTimestamp(row[1]),
			Actor:      row[2],
			Action:     row[3],
			TargetID:   row[4],
			TargetType: row[5],
		}
		if row[6] != "" {
			var detail map[string]interface{}
			if json.Unmarshal([]byte(row[6]), &detail) == nil {
				entry.Detail = detail
			}
		}
		out = append(out, entry)
	}
	return out, nil
}

// parseTimestamp converts a PostgreSQL TIMESTAMPTZ text value to RFC3339.
// Mirrors the format list in internal/pg's scanValue.
func parseTimestamp(s string) string {
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999999+00",
		"2006-01-02 15:04:05+00",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return s // return as-is if no layout matches
}

func nullIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
