# Threat Model — Financial Crime AI Red Team

## Scope
This threat model covers the sandboxed platform itself, not the AML systems it red-teams.
All data is synthetic; no real PII or financial records are in scope.

---

## Assets
| Asset | Sensitivity | Rationale |
|---|---|---|
| Synthetic dataset | Low | No real PII; risk is research IP |
| Gemini prompt/response logs | Medium | May reveal red-team strategy |
| Evidence bundles | Medium | Exposes AML rule gaps to attacker |
| Firebase credentials | High | Admin access to frontend deployment |
| Cloud SQL credentials | High | Full DB access |
| Vertex AI service account key | High | Unbounded LLM spend + data exfil |

---

## Threat Actors
1. **External attacker** — No assumed network access to Cloud SQL (private VPC)
2. **Insider / compromised engineer** — Has GCP project access
3. **Prompt injection via Gemini** — Malicious content in AML scenario causes Gemini to produce harmful output

---

## Threat Table (STRIDE)

| ID | Threat | Category | Component | Mitigation |
|---|---|---|---|---|
| T-01 | Unauthenticated API access | Spoofing | `cmd/api` | Firebase ID token verification on all routes |
| T-02 | Horizontal privilege escalation between org units | Spoofing | `cmd/api` | Org-scoped queries; **DECISION-003** (multi-org) not resolved yet |
| T-03 | SQL injection via cohort predicate DSL | Tampering | `internal/bank` | Parameterised queries only; DSL → whitelist AST, never raw SQL |
| T-04 | Prompt injection in scenario Gemini call | Tampering | `internal/scenario` | Structured prompt with system instructions; response schema validation |
| T-05 | Evidence bundle tamper (JSONB mutation) | Tampering | `internal/evidence` | SHA-256 checksum stored separately; regression runner validates checksum |
| T-06 | Gemini response exfiltrates raw AML rule logic | Information Disclosure | `internal/scenario` | Prompt never includes actual rule source code; only coverage metadata |
| T-07 | Cloud SQL credentials in environment leak | Information Disclosure | `cmd/api` | Secrets via Secret Manager; `.env` never committed; AGENTS.md enforces |
| T-08 | Unbounded Gemini spend via API flood | Denial of Service | `internal/scenario` | Rate-limit scenario proposals per cohort; human approval gate |
| T-09 | Worker job loop runaway (infinite retry) | Denial of Service | `cmd/worker` | Exponential backoff; max_attempts=5 on job record |
| T-10 | Scenario approval bypassed programmatically | Elevation of Privilege | `cmd/api` | `PATCH /scenarios/{id}/approve` requires authenticated user; status FSM enforced in DB |

---

## Trust Boundaries
```
[Browser]  →  [Firebase Hosting / CDN]  →  [Cloud Run API]  →  [Cloud SQL]
                                                            →  [Vertex AI]
[Cloud Run Worker]  →  [Cloud SQL]
```
- No direct browser → DB connection
- No direct browser → Vertex AI connection
- Worker has no inbound network surface

---

## Residual Risks (Unmitigated, Tracked)
- **T-02 partial**: Multi-org isolation not designed yet (DECISION-003)
- **Prompt injection (T-04)**: Structured schema reduces but does not eliminate risk; no content filter layer yet
- **Audit logging**: No centralised audit log of approvals/rejections yet (DECISION-006)
