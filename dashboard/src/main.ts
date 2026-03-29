// open-cognition dashboard
// Read-only view of system state, canonical objects, agent references, and audit log.
// Fetches from the control plane API and auto-refreshes every 30 seconds.

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

interface SystemState {
  mode: "RUNNING" | "STOPPED";
  changed_by: string;
  changed_at: string;
}

interface CanonicalObject {
  id: string;
  object_type: string;
  content_type: string;
  size_bytes: number;
  created_by: string;
  created_at: string;
  storage_path: string;
  metadata?: Record<string, unknown>;
}

interface AgentReference {
  id: string;
  canonical_object_id: string;
  agent_id: string;
  created_at: string;
  context: string;
  relevance?: number;
  trust_weight?: number;
  signature?: string;
}

interface AuditEntry {
  id: number;
  occurred_at: string;
  actor: string;
  action: string;
  target_id?: string;
  target_type?: string;
  detail?: Record<string, unknown>;
}

// ---------------------------------------------------------------------------
// API client
// ---------------------------------------------------------------------------

const BASE = "";

async function apiGet<T>(path: string): Promise<T> {
  const res = await fetch(BASE + path);
  if (!res.ok) {
    throw new Error(`${path}: HTTP ${res.status}`);
  }
  return res.json() as Promise<T>;
}

async function fetchStatus(): Promise<SystemState> {
  return apiGet<SystemState>("/status");
}

async function fetchCanonicals(limit = 50): Promise<CanonicalObject[]> {
  const data = await apiGet<CanonicalObject[] | null>(`/canonicals?limit=${limit}`);
  return data ?? [];
}

async function fetchReferences(limit = 50): Promise<AgentReference[]> {
  const data = await apiGet<AgentReference[] | null>(`/references?limit=${limit}`);
  return data ?? [];
}

async function fetchAudit(limit = 30): Promise<AuditEntry[]> {
  const data = await apiGet<AuditEntry[] | null>(`/audit?limit=${limit}`);
  return data ?? [];
}

// ---------------------------------------------------------------------------
// Utilities
// ---------------------------------------------------------------------------

function esc(s: string | undefined | null): string {
  if (s == null) return "";
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;");
}

function shortId(id: string): string {
  // sha256:abcdef... → sha256:abcdef (first 12 hex chars)
  const m = id.match(/^(sha256:)([0-9a-f]{12})/);
  if (m) return `${m[1]}${m[2]}…`;
  // UUID: first 8 chars
  const u = id.match(/^([0-9a-f]{8})-/);
  if (u) return `${u[1]}…`;
  return id.length > 16 ? id.slice(0, 14) + "…" : id;
}

function fmtTime(iso: string): string {
  if (!iso) return "—";
  try {
    const d = new Date(iso);
    return d.toISOString().replace("T", " ").replace(/\.\d{3}Z$/, "Z");
  } catch {
    return iso;
  }
}

function fmtBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}

function el(id: string): HTMLElement {
  return document.getElementById(id) as HTMLElement;
}

// ---------------------------------------------------------------------------
// Renderers
// ---------------------------------------------------------------------------

function renderStatus(state: SystemState): void {
  const banner = el("status-banner");
  const running = state.mode === "RUNNING";
  banner.className = `status-banner ${running ? "running" : "stopped"}`;
  banner.innerHTML = `
    <span class="mode-dot"></span>
    <span class="mode-label">${esc(state.mode)}</span>
    <span class="mode-meta">
      changed by <strong>${esc(state.changed_by)}</strong>
      at ${fmtTime(state.changed_at)}
    </span>
  `;
  document.title = `[${state.mode}] open-cognition`;
}

function renderCanonicals(objects: CanonicalObject[]): void {
  const section = el("canonicals-section");
  el("canonicals-count").textContent = String(objects.length);

  if (objects.length === 0) {
    section.querySelector(".table-wrap")!.innerHTML =
      '<p class="empty">No canonical objects yet.</p>';
    return;
  }

  const rows = objects
    .map(
      (o) => `
    <tr>
      <td class="mono" title="${esc(o.id)}">${esc(shortId(o.id))}</td>
      <td><span class="badge type-${esc(o.object_type)}">${esc(o.object_type)}</span></td>
      <td>${esc(o.content_type)}</td>
      <td class="right">${fmtBytes(o.size_bytes)}</td>
      <td>${esc(o.created_by)}</td>
      <td class="nowrap">${fmtTime(o.created_at)}</td>
    </tr>`
    )
    .join("");

  section.querySelector(".table-wrap")!.innerHTML = `
    <table>
      <thead>
        <tr>
          <th>ID</th><th>Type</th><th>Content-Type</th>
          <th class="right">Size</th><th>Created By</th><th>Created At</th>
        </tr>
      </thead>
      <tbody>${rows}</tbody>
    </table>`;
}

function renderReferences(refs: AgentReference[]): void {
  const section = el("references-section");
  el("references-count").textContent = String(refs.length);

  if (refs.length === 0) {
    section.querySelector(".table-wrap")!.innerHTML =
      '<p class="empty">No agent references yet.</p>';
    return;
  }

  const rows = refs
    .map(
      (r) => `
    <tr>
      <td class="mono" title="${esc(r.id)}">${esc(shortId(r.id))}</td>
      <td class="mono" title="${esc(r.canonical_object_id)}">${esc(
        shortId(r.canonical_object_id)
      )}</td>
      <td>${esc(r.agent_id)}</td>
      <td class="context" title="${esc(r.context)}">${esc(r.context)}</td>
      <td class="right">${r.relevance != null ? r.relevance.toFixed(2) : "—"}</td>
      <td class="nowrap">${fmtTime(r.created_at)}</td>
    </tr>`
    )
    .join("");

  section.querySelector(".table-wrap")!.innerHTML = `
    <table>
      <thead>
        <tr>
          <th>Ref ID</th><th>Object ID</th><th>Agent</th>
          <th>Context</th><th class="right">Relevance</th><th>Created At</th>
        </tr>
      </thead>
      <tbody>${rows}</tbody>
    </table>`;
}

function renderAudit(entries: AuditEntry[]): void {
  const section = el("audit-section");
  el("audit-count").textContent = String(entries.length);

  if (entries.length === 0) {
    section.querySelector(".table-wrap")!.innerHTML =
      '<p class="empty">No audit entries yet.</p>';
    return;
  }

  const rows = entries
    .map((e) => {
      const detail = e.detail
        ? esc(JSON.stringify(e.detail))
        : "—";
      const target = e.target_id
        ? `<span title="${esc(e.target_id)}">${esc(shortId(e.target_id))}</span>`
        : "—";
      return `
    <tr>
      <td class="nowrap">${fmtTime(e.occurred_at)}</td>
      <td>${esc(e.actor)}</td>
      <td><span class="badge action-${esc(e.action)}">${esc(e.action)}</span></td>
      <td>${target}</td>
      <td class="detail">${detail}</td>
    </tr>`;
    })
    .join("");

  section.querySelector(".table-wrap")!.innerHTML = `
    <table>
      <thead>
        <tr>
          <th>Time</th><th>Actor</th><th>Action</th>
          <th>Target</th><th>Detail</th>
        </tr>
      </thead>
      <tbody>${rows}</tbody>
    </table>`;
}

function renderError(message: string): void {
  el("error-bar").textContent = message;
  el("error-bar").style.display = "block";
}

function clearError(): void {
  el("error-bar").style.display = "none";
}

// ---------------------------------------------------------------------------
// Refresh loop
// ---------------------------------------------------------------------------

const REFRESH_INTERVAL = 30;
let countdown = REFRESH_INTERVAL;
let timer: ReturnType<typeof setInterval> | null = null;

async function refresh(): Promise<void> {
  el("refresh-btn").setAttribute("disabled", "true");
  countdown = REFRESH_INTERVAL;
  clearError();

  try {
    const [status, canonicals, refs, audit] = await Promise.all([
      fetchStatus(),
      fetchCanonicals(),
      fetchReferences(),
      fetchAudit(),
    ]);
    renderStatus(status);
    renderCanonicals(canonicals);
    renderReferences(refs);
    renderAudit(audit);
    el("last-updated").textContent = `Updated ${fmtTime(new Date().toISOString())}`;
  } catch (err) {
    renderError(`Fetch failed: ${err instanceof Error ? err.message : String(err)}`);
  } finally {
    el("refresh-btn").removeAttribute("disabled");
  }
}

function startCountdown(): void {
  if (timer) clearInterval(timer);
  timer = setInterval(() => {
    countdown--;
    el("countdown").textContent = `${countdown}s`;
    if (countdown <= 0) {
      countdown = REFRESH_INTERVAL;
      refresh();
    }
  }, 1000);
}

// ---------------------------------------------------------------------------
// Entry point
// ---------------------------------------------------------------------------

document.addEventListener("DOMContentLoaded", () => {
  el("refresh-btn").addEventListener("click", () => {
    refresh();
    startCountdown(); // reset timer after manual refresh
  });

  refresh();
  startCountdown();
});
