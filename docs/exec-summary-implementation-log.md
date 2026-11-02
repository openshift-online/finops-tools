# Executive Summary PR1 — Implementation Log

## 2026-11-02 — PR1 vertical slice

- **Phase:** PR1 (single-payer AWS executive summary)
- **Branch:** `exec-summary-phase-1`
- **Starting commit:** `c50f48034e75d0a159fff4e76442bdfaf1cf98e8` (`upstream/main`; HEAD matched at start; tracked tree clean)
- **Plan:** `/Users/aaronrhoney/Desktop/exec-summary/exec-summary-pr1-agent-context.md`
- **Approvals collected (user, 2026-11-02):** service dimension = native AWS Cost Explorer `SERVICE` only (no CSV mapping in PR1); CLI `finops report create exec-summary --payer <alias> [--months 5] [--output <path>]` with options scoped to this report; sub-cent ($0.01) reconciliation tolerance; Monthly Total = payer + active members for the latest completed month; MoM = calendar-day-normalized daily averages (N/A for first month and zero prior daily average).

### Files and behavior changed

| Commit | Change |
|---|---|
| `4095922` | `core/cost/monthly_grouped.go` (+tests): `sumNetAmortizedMonthlyGrouped` — MONTHLY granularity, `NetAmortizedCost`, two-dimension group-by `LINKED_ACCOUNT` then `SERVICE`, full pagination, mixed-currency and amount-parse failures, deterministic ordering. |
| `55397dc` | `core/cost/monthly_account_service.go` (+tests): `FetchMonthlyAccountService` — batches linked member IDs (≤100 per CE filter), always collects payer own spend through a separate payer-ID-filtered grouped query (payer included exactly once), single-payer and single-currency enforcement, per-month reconciliation of grouped sums against selected-account monthly totals within $0.01. |
| `9504e33` | `core/report/execsummary/report.go` (+tests): `BuildReport` — monthly totals, calendar-day-normalized MoM (nil = N/A for first month / zero prior daily average), payer-flagged account summaries ordered by window spend, retained per-account service rows; rows outside the window fail the build. |
| `76a59e4` | `cli/internal/report`: `exec-summary` template registration, generator (completed-month window via `execsummary.ComputeWindow`, default 5 months), HTML renderer with Monthly Total and MoM Growth KPI cards, per-account monthly totals with a distinct payer row, and the monthly account/service table. Money formatted only at render time; all labels escaped by `html/template`. |
| `dad756c` | `cli/internal/cmd`: `finops report create exec-summary` wiring — requires `--payer` alone, rejects `--days/--from/--to/--exclude-recent-days` (only the shared `--months` flag, interpreted as completed calendar months), prepends the payer target to the active linked member targets. |

### Design decisions (rulings)

1. **Payer inclusion made unconditional:** the payer's own spend is always fetched via a separate `LINKED_ACCOUNT = payerID` grouped query and the payer ID is excluded from member batches. The plan's "fallback if grouped response omits payer ID" is applied unconditionally, so payer inclusion never depends on unverifiable grouped-row behavior and cannot double count.
2. **Reconciliation scope:** grouped sums are reconciled against monthly totals filtered to the *selected* accounts (payer + active members), not the org-unfiltered total — the unfiltered total includes spend from accounts outside the active selection and would produce false failures.
3. **`--months` flag reuse:** `report create` already has a shared `--months` period flag; for exec-summary it counts completed calendar months (default 5) and the other period flags are rejected.

### Live payer verification gate (release gate) — 2026-11-02

Read-only checks through the approved AWS wrapper (`aws ce get-cost-and-usage`), configured payer profile, month 2026-10 (half-open complete-month interval), identical dates for all three requests:

1. Unfiltered monthly total (`NetAmortizedCost`).
2. Monthly total grouped by `LINKED_ACCOUNT` and `SERVICE`, all pages consumed.
3. Payer-ID-only monthly total filtered with `LINKED_ACCOUNT = payerID`.

**Recorded outcome (month 2026-10, currency USD):** the payer ID appears in the grouped results; the grouped sum, the payer-filtered amount, and the unfiltered total reconcile with one another within the sub-cent tolerance (grouped vs unfiltered difference ≈ 0; payer-filtered matches the grouped payer rows). The query shape matched the plan exactly (MONTHLY, `NetAmortizedCost`, `LINKED_ACCOUNT` then `SERVICE`). Pagination was required (multiple pages); note the `aws` CLI did not auto-paginate this operation — the token must be passed via `--next-page-token` — which affects ad-hoc CLI verification only; the Go client paginates explicitly.

Per the plan, no credentials, tokens, account IDs, or cost figures are recorded here.

### End-to-end live run — 2026-11-02

`bin/finops report create exec-summary --payer <configured-alias> --output <temp path>` was run read-only through the same payer credentials. **Outcome: success.** The report covered the default 5 completed months for the configured payer and its active linked accounts (four-digit account count, i.e. more than 100 members, so the ≤100 linked-account batch limit and Cost Explorer pagination were both exercised on the real org), monthly reconciliation passed (no error), and the HTML was written with the expected sections (Monthly Total and MoM Growth KPIs, per-account monthly totals, monthly account/service breakdown). The output file is large (double-digit MB) because the drill-down table includes every account/service/month row for the whole org; limiting that table is deferred to a later PR.

### Validation

Commands run and results (2026-11-02):

- `go test ./core/cost` → ok
- `go test ./core/report/execsummary` → ok
- `go test ./internal/report ./internal/cmd` (cli module) → ok
- `go vet ./internal/cmd ./internal/report` (cli module) → ok
- `make test` → FAIL in `cli/internal/aws` only: `TestCheckCredentialsNotFound`, `TestResolveCredentialsAbsentProfile`. **Pre-existing on the baseline commit `c50f480`** (verified in a clean baseline worktree; environment-dependent — the local machine has real AWS profiles). Not caused by this PR.
- `make build` → ok (`bin/finops` built)
- `go build ./core/...` and cli module `go build ./...` → ok
- `git diff --check` → clean

### Limitations and remaining risks

- The live gate was run for one month on one payer; earlier months were not live-verified, though the code path is month-independent.
- Cost Explorer data lag can make recent completed months incomplete; the report labels the period and fails on collection errors rather than emitting partial data.
- Deferred to later phases per plan: HCP hierarchy, Savings Plans, anomalies, forecast, multi-payer, CSV category mapping.
