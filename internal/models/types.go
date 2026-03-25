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
