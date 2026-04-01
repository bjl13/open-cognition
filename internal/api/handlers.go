package api

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"time"

	"github.com/bjl13/open-cognition/internal/db"
	"github.com/bjl13/open-cognition/internal/models"
	"github.com/bjl13/open-cognition/internal/storage"
)

var (
	sha256RE = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	// storage_path must match canonical/{object_type}/{yyyy}/{mm}/{dd}/{id}.json
	storagePathRE = regexp.MustCompile(`^canonical/[a-z_]+/[0-9]{4}/[0-9]{2}/[0-9]{2}/sha256:[0-9a-f]{64}\.json$`)
	// UUID v4: variant 10xx, version 0100
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
	db      *db.DB
	storage *storage.Client
}

// NewHandler constructs a Handler.
func NewHandler(database *db.DB, store *storage.Client) *Handler {
	return &Handler{db: database, storage: store}
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
// Response helpers
// ---------------------------------------------------------------------------

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeHint(w http.ResponseWriter, status int, msg, hint string) {
	writeJSON(w, status, map[string]string{"error": msg, "hint": hint})
}

// actorFromRequest reads the actor identity from the X-Actor request header.
// Falls back to "human:unknown" — a valid but unverified identity.
func actorFromRequest(r *http.Request) string {
	if a := r.Header.Get("X-Actor"); a != "" {
		return a
	}
	return "human:unknown"
}

// guardStopped returns true (and writes a 503) when the system is STOPPED.
// All write endpoints call this before doing any work.
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
//
// Full Phase 4 flow:
//  1. Reject if STOPPED.
//  2. Decode CreateCanonicalRequest (CanonicalObject + base64 Payload).
//  3. Validate all CanonicalObject fields.
//  4. Validate Payload is non-empty.
//  5. Verify sha256(Payload) matches the id field.
//  6. Verify len(Payload) matches size_bytes.
//  7. Verify storage_path is the canonical deterministic path for this object.
//  8. Check ledger: reject 409 if this id already exists (immutability).
//  9. Check storage: reject 409 if this path already exists (belt-and-braces).
// 10. Upload Payload to storage at storage_path.
// 11. Insert metadata record into Postgres.
// 12. Write audit log entry.
// 13. Return the CanonicalObject (without the payload field).

func (h *Handler) createCanonical(w http.ResponseWriter, r *http.Request) {
	if h.guardStopped(w, r) {
		return
	}

	var req models.CreateCanonicalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if err := validateCanonicalObject(&req.CanonicalObject); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}

	// --- Payload validation ---

	if len(req.Payload) == 0 {
		writeError(w, http.StatusUnprocessableEntity, "payload is required")
		return
	}

	// Verify content hash.
	digest := sha256.Sum256(req.Payload)
	computed := fmt.Sprintf("sha256:%x", digest)
	if computed != req.ID {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("payload hash mismatch: id is %s but sha256(payload) is %s", req.ID, computed))
		return
	}

	// Verify declared size.
	if len(req.Payload) != req.SizeBytes {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("size_bytes mismatch: declared %d but payload is %d bytes", req.SizeBytes, len(req.Payload)))
		return
	}

	// Verify storage_path is the deterministic path for this object.
	createdAt, _ := time.Parse(time.RFC3339, req.CreatedAt) // already validated above
	expectedPath := fmt.Sprintf("canonical/%s/%s/%s.json",
		req.ObjectType,
		createdAt.UTC().Format("2006/01/02"),
		req.ID,
	)
	if req.StoragePath != expectedPath {
		writeError(w, http.StatusUnprocessableEntity,
			fmt.Sprintf("storage_path must be %q", expectedPath))
		return
	}

	// --- Immutability checks ---

	// Ledger check (fast path — in-cluster).
	ledgerExists, err := h.db.CanonicalObjectExists(r.Context(), req.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check ledger")
		return
	}
	if ledgerExists {
		writeError(w, http.StatusConflict, "canonical object with this id already exists")
		return
	}

	// Storage check (belt-and-braces — catches any orphaned objects from a
	// previous failed transaction where storage succeeded but DB did not).
	storageExists, err := h.storage.ObjectExists(r.Context(), req.StoragePath)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to check object storage")
		return
	}
	if storageExists {
		writeError(w, http.StatusConflict,
			"object already exists in storage (orphaned write?); inspect and reconcile manually")
		return
	}

	// --- Write ---

	// Storage first: if this succeeds and the DB write fails, the orphan is
	// detectable via the storage check above on the next attempt. A background
	// reconciliation process can clean these up in a future phase.
	if err := h.storage.PutObject(r.Context(), req.StoragePath, req.Payload, req.ContentType); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write to object storage: "+err.Error())
		return
	}

	if err := h.db.InsertCanonicalObject(r.Context(), &req.CanonicalObject); err != nil {
		// The object is now in storage but not in the ledger. Log loudly.
		// A reconciliation process can re-drive the DB insert from storage.
		writeError(w, http.StatusInternalServerError,
			"payload stored but ledger insert failed — object may need reconciliation: "+err.Error())
		return
	}

	_ = h.db.WriteAuditLog(r.Context(), req.CreatedBy, "create_canonical", req.ID, "canonical_object",
		map[string]interface{}{
			"object_type":  req.ObjectType,
			"size_bytes":   req.SizeBytes,
			"storage_path": req.StoragePath,
		})

	writeJSON(w, http.StatusCreated, req.CanonicalObject)
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

	// Referential integrity: the target canonical object must be in the ledger.
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

	// --- Signature verification ---

	const sigHint = "sign the string '{id}:{canonical_object_id}:{agent_id}:{created_at}' " +
		"with your Ed25519 key and include 'signature' (base64 64-byte sig) and " +
		"'public_key' (base64 raw 32-byte key) in the request"

	if ref.Signature == "" {
		writeHint(w, http.StatusUnprocessableEntity, "signature is required", sigHint)
		return
	}
	if ref.PublicKey == "" {
		writeHint(w, http.StatusUnprocessableEntity, "public_key is required", sigHint)
		return
	}

	pubKeyBytes, err := base64.StdEncoding.DecodeString(ref.PublicKey)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		writeHint(w, http.StatusUnprocessableEntity,
			"public_key must be a base64-encoded 32-byte Ed25519 public key", sigHint)
		return
	}

	sigBytes, err := base64.StdEncoding.DecodeString(ref.Signature)
	if err != nil || len(sigBytes) != ed25519.SignatureSize {
		writeHint(w, http.StatusUnprocessableEntity,
			"signature must be a base64-encoded 64-byte Ed25519 signature", sigHint)
		return
	}

	message := []byte(ref.ID + ":" + ref.CanonicalObjectID + ":" + ref.AgentID + ":" + ref.CreatedAt)
	if !ed25519.Verify(ed25519.PublicKey(pubKeyBytes), message, sigBytes) {
		writeError(w, http.StatusUnprocessableEntity, "signature verification failed")
		return
	}

	storedKey, _, err := h.db.LookupOrRegisterAgentKey(r.Context(), ref.AgentID, ref.PublicKey, ref.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to register agent key")
		return
	}
	if storedKey != ref.PublicKey {
		writeHint(w, http.StatusUnprocessableEntity,
			"public key mismatch",
			fmt.Sprintf("agent %q is already registered with a different public key; "+
				"resubmit with the original key or ask the operator to update agent_keys", ref.AgentID))
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
