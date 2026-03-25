package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/bjl13/open-cognition/internal/db"
	"github.com/bjl13/open-cognition/internal/models"
)

var (
	sha256RE      = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	storagePathRE = regexp.MustCompile(`^canonical/[a-z_]+/[0-9]{4}/[0-9]{2}/[0-9]{2}/sha256:[0-9a-f]{64}\.json$`)
	// UUID v4: variant bits 10xx, version bits 0100
	uuidV4RE = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

	validObjectTypes = map[string]bool{
		"observation": true,
		"document":    true,
		"tool_output": true,
		"policy":      true,
	}
)

// Handler holds shared dependencies for all HTTP handlers.
type Handler struct {
	db *db.DB
}

// NewHandler constructs a Handler.
func NewHandler(database *db.DB) *Handler {
	return &Handler{db: database}
}

// RegisterRoutes registers all five control-plane endpoints on mux.
// Go 1.22 method+path syntax is used for exact matching.
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /status", h.getStatus)
	mux.HandleFunc("POST /stop", h.stop)
	mux.HandleFunc("POST /resume", h.resume)
	mux.HandleFunc("POST /canonical", h.createCanonical)
	mux.HandleFunc("POST /reference", h.createReference)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// actorFromRequest reads the actor identity from the X-Actor header.
// Falls back to "human:unknown" — a valid but unverified identity.
func actorFromRequest(r *http.Request) string {
	if a := r.Header.Get("X-Actor"); a != "" {
		return a
	}
	return "human:unknown"
}

// guardStopped returns true (and writes a 503) when the system is STOPPED.
func (h *Handler) guardStopped(w http.ResponseWriter, r *http.Request) bool {
	state, err := h.db.GetSystemState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read system state")
		return true
	}
	if state.Mode == "STOPPED" {
		writeError(w, http.StatusServiceUnavailable, "system is STOPPED; no writes accepted")
		return true
	}
	return false
}

// ---------------------------------------------------------------------------
// GET /status
// ---------------------------------------------------------------------------

func (h *Handler) getStatus(w http.ResponseWriter, r *http.Request) {
	state, err := h.db.GetSystemState(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read system state")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

// ---------------------------------------------------------------------------
// POST /stop
// ---------------------------------------------------------------------------

func (h *Handler) stop(w http.ResponseWriter, r *http.Request) {
	actor := actorFromRequest(r)
	state, err := h.db.SetSystemMode(r.Context(), "STOPPED", actor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update system mode")
		return
	}
	_ = h.db.WriteAuditLog(r.Context(), actor, "stop", "", "system_state",
		map[string]interface{}{"mode": "STOPPED"})
	writeJSON(w, http.StatusOK, state)
}

// ---------------------------------------------------------------------------
// POST /resume
// ---------------------------------------------------------------------------

func (h *Handler) resume(w http.ResponseWriter, r *http.Request) {
	actor := actorFromRequest(r)
	state, err := h.db.SetSystemMode(r.Context(), "RUNNING", actor)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update system mode")
		return
	}
	_ = h.db.WriteAuditLog(r.Context(), actor, "resume", "", "system_state",
		map[string]interface{}{"mode": "RUNNING"})
	writeJSON(w, http.StatusOK, state)
}

// ---------------------------------------------------------------------------
// POST /canonical
// ---------------------------------------------------------------------------

func (h *Handler) createCanonical(w http.ResponseWriter, r *http.Request) {
	if h.guardStopped(w, r) {
		return
	}

	var obj models.CanonicalObject
	if err := json.NewDecoder(r.Body).Decode(&obj); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := validateCanonicalObject(&obj); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// Canonical objects are immutable — reject duplicate IDs.
	exists, err := h.db.CanonicalObjectExists(r.Context(), obj.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check for existing object")
		return
	}
	if exists {
		writeError(w, http.StatusConflict, "canonical object with this id already exists")
		return
	}

	if err := h.db.InsertCanonicalObject(r.Context(), &obj); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to insert canonical object")
		return
	}
	_ = h.db.WriteAuditLog(r.Context(), obj.CreatedBy, "create_canonical", obj.ID, "canonical_object",
		map[string]interface{}{"object_type": obj.ObjectType})

	writeJSON(w, http.StatusCreated, obj)
}

// ---------------------------------------------------------------------------
// POST /reference
// ---------------------------------------------------------------------------

func (h *Handler) createReference(w http.ResponseWriter, r *http.Request) {
	if h.guardStopped(w, r) {
		return
	}

	var ref models.AgentReference
	if err := json.NewDecoder(r.Body).Decode(&ref); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := validateAgentReference(&ref); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// Referential integrity: the target canonical object must exist.
	exists, err := h.db.CanonicalObjectExists(r.Context(), ref.CanonicalObjectID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check canonical object")
		return
	}
	if !exists {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("canonical_object_id %q does not exist in ledger", ref.CanonicalObjectID))
		return
	}

	if err := h.db.InsertAgentReference(r.Context(), &ref); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to insert agent reference")
		return
	}
	_ = h.db.WriteAuditLog(r.Context(), ref.AgentID, "create_reference", ref.ID, "agent_reference",
		map[string]interface{}{"canonical_object_id": ref.CanonicalObjectID})

	writeJSON(w, http.StatusCreated, ref)
}

// ---------------------------------------------------------------------------
// Validation
// ---------------------------------------------------------------------------

func validateCanonicalObject(obj *models.CanonicalObject) error {
	if obj.SchemaVersion != "0.1.0" {
		return fmt.Errorf("schema_version must be '0.1.0'")
	}
	if !sha256RE.MatchString(obj.ID) {
		return fmt.Errorf("id must match sha256:[0-9a-f]{64}")
	}
	if !validObjectTypes[obj.ObjectType] {
		return fmt.Errorf("object_type must be one of: observation, document, tool_output, policy")
	}
	if obj.ContentType == "" {
		return fmt.Errorf("content_type is required")
	}
	if obj.SizeBytes < 0 {
		return fmt.Errorf("size_bytes must be >= 0")
	}
	if obj.CreatedBy == "" {
		return fmt.Errorf("created_by is required")
	}
	if _, err := time.Parse(time.RFC3339, obj.CreatedAt); err != nil {
		return fmt.Errorf("created_at must be a valid RFC3339 timestamp")
	}
	if !storagePathRE.MatchString(obj.StoragePath) {
		return fmt.Errorf("storage_path must match canonical/{object_type}/{yyyy}/{mm}/{dd}/{id}.json")
	}
	return nil
}

func validateAgentReference(ref *models.AgentReference) error {
	if ref.SchemaVersion != "0.1.0" {
		return fmt.Errorf("schema_version must be '0.1.0'")
	}
	if !uuidV4RE.MatchString(ref.ID) {
		return fmt.Errorf("id must be a valid UUID v4")
	}
	if !sha256RE.MatchString(ref.CanonicalObjectID) {
		return fmt.Errorf("canonical_object_id must match sha256:[0-9a-f]{64}")
	}
	if ref.AgentID == "" {
		return fmt.Errorf("agent_id is required")
	}
	if ref.Context == "" {
		return fmt.Errorf("context is required")
	}
	if _, err := time.Parse(time.RFC3339, ref.CreatedAt); err != nil {
		return fmt.Errorf("created_at must be a valid RFC3339 timestamp")
	}
	if ref.Relevance != nil && (*ref.Relevance < 0 || *ref.Relevance > 1) {
		return fmt.Errorf("relevance must be between 0.0 and 1.0")
	}
	if ref.TrustWeight != nil && (*ref.TrustWeight < 0 || *ref.TrustWeight > 1) {
		return fmt.Errorf("trust_weight must be between 0.0 and 1.0")
	}
	return nil
}
