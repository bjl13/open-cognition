package models

// SystemState mirrors the single-row system_state table.
type SystemState struct {
	Mode      string `json:"mode"`
	ChangedBy string `json:"changed_by"`
	ChangedAt string `json:"changed_at"`
}

// CanonicalObject is the truth layer: an immutable, content-addressed record.
// All fields mirror the canonical_object.schema.json definition.
type CanonicalObject struct {
	SchemaVersion string                 `json:"schema_version"`
	ID            string                 `json:"id"`
	ObjectType    string                 `json:"object_type"`
	ContentType   string                 `json:"content_type"`
	SizeBytes     int                    `json:"size_bytes"`
	CreatedAt     string                 `json:"created_at"`
	CreatedBy     string                 `json:"created_by"`
	StoragePath   string                 `json:"storage_path"`
	Metadata      map[string]interface{} `json:"metadata,omitempty"`
}

// CreateCanonicalRequest is the wire format for POST /canonical.
// It extends CanonicalObject with a Payload field that carries the raw bytes
// to be content-addressed and stored.
//
// Payload is serialised as base64 by encoding/json ([]byte convention).
// The control plane verifies sha256(Payload) == ID and len(Payload) == SizeBytes
// before writing anything to storage or the ledger.
//
// Payload is not persisted in Postgres; it goes to object storage only.
// The canonical_object.schema.json definition remains unchanged — Payload is
// an upload-time envelope field.
type CreateCanonicalRequest struct {
	CanonicalObject
	Payload []byte `json:"payload"` // required; base64-encoded raw bytes
}

// AuditEntry is one row from the append-only audit_log table.
type AuditEntry struct {
	ID         int64                  `json:"id"`
	OccurredAt string                 `json:"occurred_at"`
	Actor      string                 `json:"actor"`
	Action     string                 `json:"action"`
	TargetID   string                 `json:"target_id,omitempty"`
	TargetType string                 `json:"target_type,omitempty"`
	Detail     map[string]interface{} `json:"detail,omitempty"`
}

// AgentReference is the meaning layer: an agent-scoped pointer to a
// canonical object. All fields mirror agent_reference.schema.json.
type AgentReference struct {
	SchemaVersion     string                 `json:"schema_version"`
	ID                string                 `json:"id"`
	CanonicalObjectID string                 `json:"canonical_object_id"`
	AgentID           string                 `json:"agent_id"`
	CreatedAt         string                 `json:"created_at"`
	Context           string                 `json:"context"`
	Relevance         *float64               `json:"relevance,omitempty"`
	TrustWeight       *float64               `json:"trust_weight,omitempty"`
	TimeHorizon       string                 `json:"time_horizon,omitempty"`
	Signature         string                 `json:"signature,omitempty"`
	Metadata          map[string]interface{} `json:"metadata,omitempty"`
}
