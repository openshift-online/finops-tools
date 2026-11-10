# finops-tools

FinOps command-line tools. The repository is a Go monorepo with a shared **core** library and a **finops** CLI built with [Cobra](https://github.com/spf13/cobra).

## Layout

| Path | Module | Role |
|------|--------|------|
| `core/` | `github.com/openshift-online/finops-tools/core` | Business logic (no CLI/HTTP dependencies) |
| `cli/` | `github.com/openshift-online/finops-tools/cli` | Cobra commands; calls into `core` |
| `backend/` | `github.com/openshift-online/finops-tools/backend` | HTTP server; calls into `core` |
| `go.work` | — | Ties modules together for local development |

The HTTP API is a separate module that imports **`core` only** — it must never depend on `cli` (enforced by `backend/boundary_test.go`).

### Package map (`cli/internal`)

| Package | Role |
|---------|------|
| `cmd/` | Cobra wiring only: `<noun>.go` + `<noun>_<verb>.go` (e.g. `report_create.go`) |
| `output/` | Human-readable tables and `--format` handlers |
| `format/` | Currency formatting for CLI output |
| `configstore/` | FinOps YAML config read/write |
| `snowflakeoauth/`, `snowflakecred/` | Red Hat SSO OAuth login and token storage |
| `account/` | Account login flows (`config account add` business logic; not the `cmd` noun files) |
| `aws/`, `awsauth/`, `awslogin/`, `awsrole/` | Credentials, auth orchestration, SAML, role ARNs |
| `report/` | HTML templates and charts (distinct from `core/report` data assembly) |
| `progress/` | Progress lines on stderr |

`core/` grows by domain noun (`core/cost`, `core/report`, `core/snowflake`), not by CLI verb. Shared target/credential orchestration stays in `cli/` until a third command needs it; see `.cursor/rules/cli-commands.mdc` for when to split `cmd/` into per-noun subpackages.

## CLI commands

Every command uses **singular root nouns** with **`finops <noun> <verb>`** for core ops, or **`finops config <sub-resource> <verb>`** for setup (e.g. `finops config account add`, `finops account get-cost`). See `.cursor/rules/cli-commands.mdc` for conventions when adding commands.

## Requirements

- Go 1.24+

## Development

From the repository root (uses `go.work`):

```bash
go work sync
make test
make lint
make install-hooks        # git push runs make lint (same golangci-lint as CI)
make build
make build-backend
./bin/finops --help
./bin/finops-backend      # starts HTTP server on :8080 (see HTTP API below)
```

`make lint` uses golangci-lint v2.12.2 and `.golangci.yml`, and reports only issues introduced since the merge-base with `origin/main` (local equivalent of GitHub Actions `only-new-issues` / `--new-from-patch`). Override the baseline with `make lint LINT_NEW_FROM=<sha>` to match a specific CI event. `make lint-all` scans the whole tree, including findings already on main. After `make install-hooks`, `git push` runs `make lint`. Bypass with `git push --no-verify` or `SKIP_LINT=1 git push`.

Or without Make:

```bash
go test ./core/... ./cli/... ./backend/...
go run ./cli/cmd/finops --help
go build -o bin/finops ./cli/cmd/finops
go run ./backend/cmd/finops-backend
go build -o bin/finops-backend ./backend/cmd/finops-backend
```

Edits under `core/` are picked up immediately by the CLI and HTTP server (workspace + `replace` in module `go.mod` files).

## HTTP API

The **`finops-backend`** HTTP server exposes a subset of FinOps capabilities for cluster deployment. It uses environment variables for Snowflake credentials (no local config files).

**Security note:** The MVP has **no HTTP authentication**. Restrict access at the network layer (cluster-internal Route, firewall rules) until auth and service-account Snowflake credentials are added.

OpenAPI 3.0 spec: embedded in the binary and served at `GET /openapi.yaml` (source: [`backend/internal/openapi/openapi.yaml`](backend/internal/openapi/openapi.yaml))

### Endpoints

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/openapi.yaml` | OpenAPI 3.0 specification (YAML) |
| `GET` | `/livez` | Liveness probe; process is up (no external deps) |
| `GET` | `/readyz` | Readiness probe; process can serve HTTP (does not ping Snowflake) |
| `GET` | `/health` | Alias for `/livez` |
| `POST` | `/v1/snowflake/query` | Run read-only SQL against Snowflake (`SELECT`, `WITH`, `SHOW`, `DESCRIBE`, `DESC`, `EXPLAIN`, `LIST`) |
| `GET` | `/v1/aws/accounts/historical-count` | Daily AWS linked-account counts per payer (time series) |

**Query request:**

```json
{
  "sql": "SELECT CURRENT_USER(), CURRENT_ROLE()",
  "connection": "sandbox"
}
```

The optional `connection` field selects a named Snowflake environment (see below). When omitted, the default connection is used. Clients may also pass `X-FinOps-Snowflake-Connection` when the JSON field is empty.

**Query response:**

```json
{
  "columns": ["CURRENT_USER()", "CURRENT_ROLE()"],
  "rows": [["user@example.com", "MY_ROLE"]],
  "row_count": 1,
  "truncated": false
}
```

#### AWS accounts historical count

`GET /v1/aws/accounts/historical-count` returns daily snapshots of linked AWS account counts from `HCMFINOPSSOURCE_DB.MARTS.AWS_ACCOUNTS_HISTORICAL_COUNT`. For each payer and calendar day, the API keeps the latest snapshot that day (by `TIMESTAMP`, then `RUN_ID`).

| Query param | Default | Description |
|-------------|---------|-------------|
| `from` | none | Start date `YYYY-MM-DD` (inclusive) |
| `to` | none | End date `YYYY-MM-DD` (inclusive) |
| `payer_account_id` | none | 12-digit AWS payer account filter |
| `aggregate` | `payer` | `payer` = one series per payer per day; `sum` = totals across payers per day |
| `connection` | default Snowflake connection | Query param or `X-FinOps-Snowflake-Connection` header |

**Per-payer response** (`aggregate=payer`, default):

```json
{
  "aggregate": "payer",
  "from": "2026-01-01",
  "to": "2026-03-01",
  "data": [
    {
      "date": "2026-01-19",
      "payer_account_id": "123456789012",
      "nb_active_accounts": 100,
      "nb_closed_accounts": 1,
      "nb_deleted_accounts": 0
    }
  ],
  "row_count": 1,
  "truncated": false
}
```

**Summed across payers** (`aggregate=sum`):

```json
{
  "aggregate": "sum",
  "data": [
    {
      "date": "2026-01-19",
      "nb_active_accounts": 300,
      "nb_closed_accounts": 3,
      "nb_deleted_accounts": 0
    }
  ],
  "row_count": 1,
  "truncated": false
}
```

### Environment variables

#### Server

| Variable | Required | Default | Description |
|----------|----------|---------|-------------|
| `FINOPS_BACKEND_ADDR` | no | `:8080` | Listen address |
| `FINOPS_BACKEND_MAX_ROWS` | no | `1000` | Max rows returned per generic Snowflake query |
| `FINOPS_BACKEND_QUERY_TIMEOUT` | no | `60s` | Per-query timeout |
| `FINOPS_BACKEND_AWS_ACCOUNTS_HISTORICAL_TABLE` | no | `HCMFINOPSSOURCE_DB.MARTS.AWS_ACCOUNTS_HISTORICAL_COUNT` | Snowflake table for account-count history |
| `FINOPS_BACKEND_AWS_ACCOUNTS_HISTORICAL_MAX_ROWS` | no | `10000` | Max rows returned by `/v1/aws/accounts/historical-count` |

#### Snowflake

Set `SNOWFLAKE_CONNECTIONS` to a comma-separated list of lowercase names (e.g. `sandbox`, or `preprod,sandbox` when both are configured). Each connection uses prefixed env vars: `SNOWFLAKE_CONN_<NAME>_*` where `<NAME>` is uppercased in the key (`sandbox` → `SNOWFLAKE_CONN_SANDBOX_ACCOUNT`). Omit all Snowflake variables to start without Snowflake (`/v1/snowflake/query` returns 503).

| Variable | Required | Description |
|----------|----------|-------------|
| `SNOWFLAKE_CONNECTIONS` | yes | Comma-separated connection names |
| `SNOWFLAKE_DEFAULT_CONNECTION` | no | Default when the request omits `connection` (falls back to the first listed name) |
| `SNOWFLAKE_CONN_<NAME>_ACCOUNT` | yes | Account for connection `<NAME>` |
| `SNOWFLAKE_CONN_<NAME>_USER` | yes | User for connection `<NAME>` |
| `SNOWFLAKE_CONN_<NAME>_WAREHOUSE` | yes | Warehouse for connection `<NAME>` |
| `SNOWFLAKE_CONN_<NAME>_TOKEN` or `SNOWFLAKE_CONN_<NAME>_PRIVATE_KEY` | yes | Auth for connection `<NAME>` |
| `SNOWFLAKE_CONN_<NAME>_PRIVATE_KEY_FILE` | yes* | Read PEM from a mounted file instead of inline `PRIVATE_KEY` |
| `SNOWFLAKE_CONN_<NAME>_ROLE` / `_DATABASE` / `_SCHEMA` | no | Optional session defaults |

Private keys must be **unencrypted PEM** (PKCS#8 or PKCS#1). Decrypt encrypted keys externally before mounting or inlining them (for example `openssl pkcs8 -in encrypted.pem -out decrypted.pem`).

\*Provide one of token, inline private key, or private key file per connection.

Readiness (`/readyz`) does not ping Snowflake (probes must not resume a warehouse). Connections open lazily on first query; query handlers return 503 when Snowflake is unavailable.

#### Separate Secrets per environment

OpenShift uses split Secrets per connection (see [`deploy/openshift/deployment.yaml`](deploy/openshift/deployment.yaml)): a **config** Secret loaded via `envFrom` and a **key** Secret mounted as a file. This keeps private key material out of the process environment. `SNOWFLAKE_CONNECTIONS` and `SNOWFLAKE_DEFAULT_CONNECTION` are non-secret and are set as plain env vars in the Deployment.

| Source | Contents |
|--------|----------|
| Deployment `env` | `SNOWFLAKE_CONNECTIONS`, `SNOWFLAKE_DEFAULT_CONNECTION` |
| `finops-backend-snowflake-preprod-config` | `SNOWFLAKE_CONN_PREPROD_*` env keys (including `PRIVATE_KEY_FILE` path) |
| `finops-backend-snowflake-preprod-key` | `private_key` file mounted at `/etc/finops/snowflake/preprod/` |
| `finops-backend-snowflake-sandbox-config` | `SNOWFLAKE_CONN_SANDBOX_*` env keys (including `PRIVATE_KEY_FILE` path) |
| `finops-backend-snowflake-sandbox-key` | `private_key` file mounted at `/etc/finops/snowflake/sandbox/` |

Every name listed in `SNOWFLAKE_CONNECTIONS` must have matching config and key Secrets before the backend starts.

See [`deploy/openshift/secret.yaml.example`](deploy/openshift/secret.yaml.example) for a full multi-Secret example.

### Local run

```bash
export SNOWFLAKE_CONNECTIONS=sandbox
export SNOWFLAKE_DEFAULT_CONNECTION=sandbox
export SNOWFLAKE_CONN_SANDBOX_ACCOUNT=example-sandbox
export SNOWFLAKE_CONN_SANDBOX_USER=example-user
export SNOWFLAKE_CONN_SANDBOX_PRIVATE_KEY="$(cat ./sandbox_rsa_key.p8)"
export SNOWFLAKE_CONN_SANDBOX_WAREHOUSE=SANDBOX_WH

make build-backend
./bin/finops-backend
```

```bash
curl -s http://localhost:8080/livez
curl -s http://localhost:8080/openapi.yaml
curl -s -X POST http://localhost:8080/v1/snowflake/query \
  -H 'Content-Type: application/json' \
  -d '{"sql":"SELECT CURRENT_USER(), CURRENT_ROLE()"}'
curl -s -X POST http://localhost:8080/v1/snowflake/query \
  -H 'Content-Type: application/json' \
  -d '{"connection":"sandbox","sql":"SELECT CURRENT_DATABASE()"}'
curl -s 'http://localhost:8080/v1/aws/accounts/historical-count?payer_account_id=123456789012&from=2026-01-01&to=2026-03-31'
curl -s 'http://localhost:8080/v1/aws/accounts/historical-count?aggregate=sum&from=2026-01-01'
```

### OpenShift deployment

Runtime namespace: **`finops-team--finops-tools-backend`** on cluster `mpp-w2-preprod`.

The TenantNamespace CR (`metadata.name: finops-tools-backend`) is applied in **`finops-team--config`**; the platform provisions **`finops-team--finops-tools-backend`**.

#### One-time namespace setup (platform / tenant admin)

Creating the namespace requires a **TenantNamespace** in `finops-team--config`. Most developers do not have permission for this API.

Ask a tenant admin to run once:

```bash
oc apply -f deploy/openshift/tenantnamespace.yaml
```

If you see `Forbidden` on `tenantnamespaces.tenant.paas.redhat.com`, you need that admin step — you cannot self-provision the namespace.

Confirm the namespace exists before deploying:

```bash
oc get namespace finops-team--finops-tools-backend
oc auth can-i create deployment -n finops-team--finops-tools-backend
```

#### Deploy the API (developers)

Full rebuild, push, and roll out:

```bash
make openshift-refresh
```

Or step by step:

1. Build and push the image:

```bash
make podman-build
make podman-push
```

2. Deploy workloads (do **not** apply `secret.yaml.example` with dummy values):

```bash
make openshift-apply
```

3. Create Snowflake Secrets when ready (config + key per connection listed in `SNOWFLAKE_CONNECTIONS`):

```bash
# Preprod (default connection)
oc create secret generic finops-backend-snowflake-preprod-config \
  --from-literal=SNOWFLAKE_CONN_PREPROD_ACCOUNT=example-preprod \
  --from-literal=SNOWFLAKE_CONN_PREPROD_USER=example-user \
  --from-literal=SNOWFLAKE_CONN_PREPROD_WAREHOUSE=PREPROD_WH \
  --from-literal=SNOWFLAKE_CONN_PREPROD_PRIVATE_KEY_FILE=/etc/finops/snowflake/preprod/private_key \
  -n finops-team--finops-tools-backend

oc create secret generic finops-backend-snowflake-preprod-key \
  --from-file=private_key=./preprod_rsa_key.p8 \
  -n finops-team--finops-tools-backend

# Sandbox
oc create secret generic finops-backend-snowflake-sandbox-config \
  --from-literal=SNOWFLAKE_CONN_SANDBOX_ACCOUNT=example-sandbox \
  --from-literal=SNOWFLAKE_CONN_SANDBOX_USER=example-user \
  --from-literal=SNOWFLAKE_CONN_SANDBOX_WAREHOUSE=SANDBOX_WH \
  --from-literal=SNOWFLAKE_CONN_SANDBOX_PRIVATE_KEY_FILE=/etc/finops/snowflake/sandbox/private_key \
  -n finops-team--finops-tools-backend

oc create secret generic finops-backend-snowflake-sandbox-key \
  --from-file=private_key=./sandbox_rsa_key.p8 \
  -n finops-team--finops-tools-backend

oc rollout restart deployment/finops-backend -n finops-team--finops-tools-backend
```

Or `make openshift-restart` after `make openshift-apply`.

4. Verify:

```bash
oc get pods,svc,endpoints,route -n finops-team--finops-tools-backend -l app=finops-backend
curl -s "https://$(oc get route finops-backend -n finops-team--finops-tools-backend -o jsonpath='{.spec.host}')/livez"
curl -s "https://$(oc get route finops-backend -n finops-team--finops-tools-backend -o jsonpath='{.spec.host}')/openapi.yaml"
```

Wire `serviceAccountName` in `deployment.yaml` when Red Hat provides the service account.

## Configuration

FinOps stores local settings in a YAML config file:

| OS | Default path |
|----|----------------|
| Linux / macOS | `$XDG_CONFIG_HOME/finops/config.yaml` or `~/.config/finops/config.yaml` |
| Windows | `%AppData%/finops/config.yaml` |

The file is created automatically on first `finops config account add`. Example:

```yaml
defaults:
  aws.auth_method: profile
  aws.linked_role: OrganizationAccountAccessRole
  cost.days: "30"
  cost.exclude_recent_days: "2"
aws:
  account_aliases:
    rh-control: "123456789012"
    osd-staging-1: "987654321098"
    osd-tenant-1:
      account_id: "111111111111"
      payer_alias: rh-control
      role: OrganizationAccountAccessRole
gcp:
  account_aliases: {}
snowflake:
  account_aliases:
    rhprod:
      account: ORG-ACCOUNT
      role: PUBLIC
      sso: prod
```

OAuth client ID and secret are **not** stored in `config.yaml`. Use a separate secrets file (default `~/.config/finops/snowflake-oauth.yaml`, mode `0600`) or environment variables.

Set defaults using fully qualified names (used when `--auth-method` is omitted on `config account add`):

```bash
finops config default set --name aws.auth-method --value profile
finops config default get --name aws.auth-method
finops config default set --name aws.linked_role --value OrganizationAccountAccessRole
finops config default set --name cost.exclude_recent_days --value 2
```

Cost query period defaults (`cost.days`, `cost.months`, `cost.from`, `cost.to`, `cost.exclude_recent_days`) apply to `finops account get-cost` and `finops report create` when the matching CLI flag is omitted. Set only one of `cost.days`, `cost.months`, or `cost.from` (optional `cost.to` with `cost.from`). `defaults.cost.exclude_recent_days` also applies to `finops account details`.

Register a **payer** account by **12-digit account ID** (login + save in config):

```bash
finops config account add aws 123456789012 --alias rh-control       # auth-method: flag, or config default, else saml
finops config account add aws 123456789012 --auth-method profile  # overrides config default
finops config account add aws 123456789012 --force
```

Register a **linked** account (authenticate to the payer first, then assume a role in the member account):

```bash
finops config account add aws 111111111111 --alias osd-tenant-1 --payer rh-control
finops config account add aws 111111111111 --payer rh-control --role CustomRole
```

The IAM role name defaults to `OrganizationAccountAccessRole`, or `defaults.aws.linked_role` in the finops config. The CLI builds `arn:aws:iam::<account-id>:role/<role-name>` automatically.

Without `--alias`, the account ID is used as the config key. Aliases are CLI-only; cost and AWS credential logic use 12-digit account IDs.

List registered accounts (payer vs linked):

```bash
finops config account list
finops config account list aws
finops config account list snowflake
```

### Snowflake (Red Hat SSO OAuth)

Query Snowflake using OAuth tokens from [Red Hat SSO](https://dataverse.pages.redhat.com/platform/snowflake/red-hat-sso-access/). The access token must include audience `dataverse-snowflake` and scope `session:role-any` (usually via IAM default client scopes / mappers, not by requesting scopes in the authorize URL). The CLI OAuth redirect URI is fixed at `http://127.0.0.1:8765/oauth/callback` (must be registered on the SSO client).

Session settings (account, role, warehouse, database, schema) are stored only in the finops config file (`~/.config/finops/config.yaml`). The CLI does **not** read `~/.snowflake/connections.toml` or Snowflake CLI connection profiles. Configure each alias with `finops config account add snowflake` flags and/or `snowflake.*` defaults below.

Store OAuth client credentials (never commit these):

```bash
finops config oauth-client set --client-id finops-tools-dataverse --client-secret "$SECRET"
# or: export FINOPS_SNOWFLAKE_OAUTH_CLIENT_ID=... FINOPS_SNOWFLAKE_OAUTH_CLIENT_SECRET=...
```

Optional defaults:

```bash
finops config default set --name snowflake.sso_issuer --value prod   # or stage (pre-prod Snowflake only)
finops config default set --name snowflake.oauth_audience --value dataverse-snowflake
# Override which registered alias finops snowflake uses (first account add sets this automatically):
# finops config default set --name snowflake.account_alias --value rhprod
# Shared session defaults when an alias omits role/warehouse/database/schema:
# finops config default set --name snowflake.warehouse --value MY_WH
# finops config default set --name snowflake.role --value MY_ROLE
```

Register a Snowflake account (opens browser for Red Hat SSO, stores refresh token in `~/.config/finops/snowflake-tokens.yaml`). A warehouse is required (per alias or via `snowflake.warehouse` default):

```bash
finops config account add snowflake myorg-sandbox --alias sandbox \
  --snowflake-role MY_ROLE \
  --warehouse MY_WH \
  --database MY_DB --schema MY_SCHEMA
finops config account add snowflake myorg-prod --alias prod --force   # re-login
```

Run SQL:

```bash
finops snowflake query --sql "SELECT CURRENT_USER(), CURRENT_ROLE()"
finops snowflake query --account-alias sandbox --sql "SELECT 1"
finops snowflake query --sql "SELECT 1" --format json
```

Manage AWS Organizations tags on an account (registered alias or 12-digit account ID):

```bash
finops tag list --account-alias rh-control
finops tag list --account-id 111111111111 --payer rh-control
finops tag add --account-alias rh-control --tag-key owner --tag-value team-a
finops tag add --account-id 111111111111 --tag-key owner --tag-value team-b --force --payer rh-control
finops tag update --account-alias rh-control --tag-key owner --tag-value team-c
finops tag update --account-id 111111111111 --tag-key env --tag-value prod --force --payer rh-control
```

List AWS Organizational Units (for discovering OU IDs to use with `--ou` on cost/report commands):

```bash
finops aws list-ous --payer rh-control
finops aws list-ous --payer rh-control --parent ou-abcd-12345678
finops aws list-ous --payer rh-control --format json
```

Move a linked account between OUs (or root) **within the same payer** (Organizations `MoveAccount`):

```bash
# Plan only
finops aws move-to-ou --payer rh-control --account-id 111111111111 --destination-ou ou-abcd-12345678 --dry-run

# Execute (requires --yes); --account-id accepts comma-separated IDs
finops aws move-to-ou --payer rh-control --account-id 111111111111 --destination-ou ou-abcd-12345678 --yes
finops aws move-to-ou --payer rh-control --account-id 111111111111,222222222222 --destination-ou ou-abcd-12345678 --yes
```

Same-payer only: billing stays with the payer; inherited SCPs / Control Tower / StackSets may change. For moving an account to a **different** payer organization, use `migrate-account` below.

Migrate a linked (member) account from one payer organization to another (AWS Organizations invite + accept):

```bash
# Plan only
finops aws migrate-account --account-id 111111111111 --from-payer rh-control --to-payer osd-staging-1 --dry-run

# Execute (requires --yes); --account-id accepts comma-separated IDs
finops aws migrate-account --account-id 111111111111 --from-payer rh-control --to-payer osd-staging-1 --yes
finops aws migrate-account --account-id 111111111111,222222222222 --from-payer rh-control --to-payer osd-staging-1 --yes
finops aws migrate-account --account-id 111111111111 --from-payer rh-control --to-payer osd-staging-1 \
  --destination-ou ou-abcd-12345678 --yes
```

`--account-id` accepts one or more comma-separated 12-digit AWS account IDs. The command invites from the destination payer, assumes into the member via the source payer (`OrganizationAccountAccessRole` or `--role`), accepts the handshake, restores Organizations account tags snapshotted from the source payer (AWS deletes them on org leave), rewrites that role's trust policy to the destination management account, optionally moves the account into a destination OU, then updates local `payer_alias`. Requires Organizations admin on both payers; SCPs/Control Tower may still block leave/accept. If accept succeeds but trust update fails, the account is already in the destination org while the role still trusts the source payer — assume from `--from-payer` and retry the trust update (not a rollback). Tag restore runs before trust/OU steps so a later failure does not leave tags unrestored. When tags are present, key/value pairs are written to `migrate-tag-recovery/<account-id>.json` next to the finops config (mode `0600`) before invite/accept and removed after a successful restore; if restore fails or the CLI exits mid-transfer, re-apply with `finops tag add` using the destination `--payer` (keys/values are trimmed; empty values are allowed).

**Cost Explorer (`finops account get-cost`) requires payer accounts only.** Linked-account credentials are for member-account APIs, not payer-level billing queries.

Static secrets (API keys, etc.) for other tools live in `~/.config/finops/.env`; AWS sessions use `~/.aws/credentials` profiles.

### AWS global flags

These persistent flags are available on AWS-backed commands (`account`, `snapshot`, `report`, `tag`, `aws`, `config account`):

| Flag | Description |
|------|-------------|
| `--auth-method` | `saml` (default) or `profile`; when omitted, uses `defaults.aws.auth_method` from config |
| `--config` | Path to finops config file (default: OS-specific config dir) |
| `--credentials-file` | Path to AWS credentials file (default: `~/.aws/credentials`) |
| `--verbose` / `-v` | Log external commands (`klist`, `curl`, …) and selected AWS API calls (STS, Organizations, EC2, RDS, EKS, ElastiCache, OpenSearch, Health, Cost Explorer) to stderr |

Verbose output uses two line prefixes: `+ "command" "args"` for external binaries and `+ AWS Service.Operation …` for cloud API calls. Organizations tag reads log `ListTagsForResource`; tag writes log `TagResource` (matching the AWS API operation names).

### AWS payer credentials

Store and verify temporary AWS credentials for a payer account (same profile layout as finops-mcp-aws):

```bash
finops config account add aws 123456789012
```

**Behavior:**

1. Looks up a profile derived from the account ID in `~/.aws/credentials`, then in `~/.aws/config` (shared config / SSO).
2. If the profile exists and STS validation succeeds, reports success without logging in again.
3. With `--auth-method saml` (default), if credentials are missing or invalid, runs a native Red Hat Kerberos + SAML login flow and merges temporary credentials into `~/.aws/credentials` (other profiles are preserved).
4. With `--auth-method profile`, SAML login is skipped. `finops config account add` uses an existing profile when valid (including a `~/.aws` profile named like `--alias`, e.g. `rh-control`); in an interactive terminal it prompts for access keys only when no matching profile exists. You can also configure the profile yourself (`aws configure`, `aws sso login`, etc.) and run `config account add` again to confirm it works.

**SAML prerequisites** (default login, and `--force`):

- Red Hat VPN connected
- Valid Kerberos ticket (`kinit`)
- Kerberos tools available locally (`klist`, usually present on managed RH laptops)

SAML account matching accepts:

- 12-digit AWS account ID (recommended for `finops config account add aws <id>`)
- SAML account display name (for example `rh-control`)
- `account/role` when a specific role name is required

#### Linked accounts (profile chaining without finops flags)

You can also configure role assumption in `~/.aws/config` and use `--auth-method profile`:

```ini
[profile rh-control]
# payer credentials (SAML output, SSO, or keys)

[profile osd-tenant-1]
role_arn = arn:aws:iam::111111111111:role/OrganizationAccountAccessRole
source_profile = rh-control
```

```bash
finops config account add aws 111111111111 --alias osd-tenant-1 --auth-method profile
```

STS validation must report the **linked** account ID. This registers credentials only; for finops metadata (`payer_alias`, `role`) use `--payer` (and optional `--role`) as shown above.

### Cost (AWS)

Fetch **Net Amortized Cost** from AWS Cost Explorer for a configurable date range (default: last 30 calendar days, or `defaults.cost.*` in config). Payer and linked account aliases are supported; linked accounts query Cost Explorer through the registered payer.

Cost queries are parameterized along two dimensions — **account selection** and **time selection**. See [`docs/query-selectors.md`](docs/query-selectors.md) for an in-depth guide to both: the four account selection modes, OU scope suffixes, the period modes and Cost Explorer lag handling, with decision flowcharts, org-tree diagrams, and worked date-range examples.

**Account selection uses exactly one mode** (combining modes is an error):

| Mode | Flags |
|------|-------|
| Explicit accounts | `--account-id` and/or `--account-alias` (optional single `--payer` for unregistered member IDs) |
| Entire org | `--payer` alone. Comma-separated aliases union members across those organizations |
| OU / org-root members | `--payer` + `--ou` (optional scope suffix per ID: `/`, `/*`, `/**`). Each OU is resolved in every listed payer |
| Tag match | `--payer` + `--tag KEY[=VALUE]`, matched in every listed payer |

```bash
finops config account add aws 123456789012 --alias rh-control
finops account get-cost --account-alias rh-control
finops account get-cost --account-alias rh-control --days 7
finops account get-cost --account-alias rh-control --months 3
finops account get-cost --account-alias rh-control --from 2026-01-01 --to 2026-03-31
finops account get-cost --account-alias rh-control --exclude-recent-days 2   # omit last 2 days (AWS CE lag)
finops account get-cost --account-alias quay              # linked account (uses payer credentials)
finops account get-cost --account-id 123456789012
finops account get-cost --account-alias rh-control,osd-staging-1
finops account get-cost --account-id 123456789012 --format json
finops account get-cost --account-id 123456789012 --format csv
finops account get-cost --account-id 123456789012 --group-by service
finops account get-cost --account-id 123456789012 --group-by account
finops account get-cost --payer rh-control --account-id 333333333333   # member account via payer
finops aws list-ous --payer rh-control           # discover OU IDs
finops account get-cost --ou ou-abcd-12345678 --payer rh-control
finops account get-cost --ou ou-abcd-12345678/ --payer rh-control --days 7
finops account get-cost --ou 'ou-abcd-12345678/*' --payer rh-control
finops account get-cost --ou r-xxxx/ --payer rh-control            # accounts directly under org root
finops account get-cost --payer rh-control
finops account get-cost --payer rh-control,osd-staging-1
finops account get-cost --payer rh-control,osd-staging-1 --tag env=prod
finops account get-cost --ou ou-abcd-12345678,ou-efgh-87654321 --payer rh-control,osd-staging-1
finops account get-cost --payer rh-control --group-by ou
finops account get-cost --payer rh-control --tag organization
finops account get-cost --payer rh-control --tag organization="Hybrid Platform" --group-by service
finops report create costs --payer rh-control --tag env=prod -o prod.html
```

| Flag | Description |
|------|-------------|
| `--account-id` | One or more comma-separated **12-digit AWS account IDs** (provider-native IDs; unregistered members require `--payer`) |
| `--account-alias` | One or more comma-separated configured aliases (e.g. `rh-control`, or a linked alias such as `quay`) |
| `--ou` | One or more comma-separated AWS OU (`ou-xxxx-yyyyy`) or org-root (`r-xxxx`) IDs; requires `--payer`. Optional scope suffix per ID: bare or `/**` = full subtree (default), `/` = accounts directly in that parent only, `/*` = parent + immediate child OUs only |
| `--payer` | Registered payer alias(es), comma-separated. Alone selects org members and unions them across the listed organizations (ACTIVE only unless `--include-closed-accounts`); also required with `--ou` / `--tag`. A single alias supplies credentials for unregistered `--account-id` values; several aliases cannot be combined with `--account-id` or `--account-alias` |
| `--tag` | Select org accounts by Organizations tag: `KEY` or `KEY=VALUE` (requires `--payer`) |
| `--include-closed-accounts` | `finops account get-cost` only. With `--ou` or `--payer` alone, also select `SUSPENDED`, `PENDING_CLOSURE`, and `CLOSED` members (default: ACTIVE only). Ignored with a warning for `--tag` (already all states) and `--account-id`/`--account-alias` |
| `--skip-org-cache` | Bypass cached organization account/tag data for `--tag` selection (always fetch live from AWS) |
| `--refresh-org-cache` | Ignore cached organization data and refresh the cache from AWS (mutually exclusive with `--skip-org-cache`) |
| `--days` | Last N calendar days (mutually exclusive with `--months` and `--from`/`--to`) |
| `--months` | Last N calendar months from the 1st of the month (mutually exclusive with `--days` and `--from`/`--to`) |
| `--from` | Start date `YYYY-MM-DD` inclusive (optional `--to`; otherwise through the latest stable day) |
| `--to` | End date `YYYY-MM-DD` inclusive (requires `--from`; historical only — future dates are rejected) |
| `--exclude-recent-days` | Omit the last N UTC days from the end anchor (incomplete AWS CE data); default from `defaults.cost.exclude_recent_days` or `0` |
| `--verbose` / `-v` | Log external commands and selected AWS API calls to stderr (see [AWS global flags](#aws-global-flags)) |
| `--format` | `pretty-print` (default), `json`, or `csv` |
| `--quiet` | Suppress progress messages on stderr (cost/CSV/JSON still go to stdout) |
| `--workers` | Maximum concurrent workers for multi-account AWS queries (default: `25`, max: `1000`; use `1` for sequential) |
| `--group-by` | Group costs by dimension: `service`, `account` (linked AWS account ID), or `ou` (OU tree under the selection root; each row is subtree total, indented in pretty-print); includes share % and relative cost bars in `pretty-print` |
| `--provider` | `aws` (default). `gcp` is reserved for a future release |

`pretty-print` uses colors and Unicode bars when stdout is a TTY. Set `NO_COLOR=1` to disable; `FORCE_COLOR=1` forces colors when piping to a capable viewer.

### Account details (AWS)

Gather **monthly cost trends**, **resource inventory** (EC2, RDS, Route53, S3, Lambda, load balancers, and more), and **OpenShift clusters** from the production Dataverse Snowflake mart (`HCMFINOPS_DB.MARTS.OCM_MAPPING`, all `ENVIRONMENT` values: Production, Stage, Integration) for one or more accounts. The same details are printed to stdout and used in owner notification emails.

The owner email is derived from the Organizations `owner` tag, appending `@redhat.com` when the tag value has no `@` sign.

By default the command prints details and does not send email. To email owners via **Gmail**, pass `--send` with either `--yes` (owner addresses) or `--redirect-prefix` (test delivery to `PREFIX+<owner>@redhat.com`). `--group-by` only affects how emails are batched; stdout is always one record per account. When sending, a delivery summary is written to stderr (pretty-print) so JSON/CSV stdout stays a pure details payload.

Owner notification email content is customizable per run (send only): `--email-subject-prefix` (plain text), `--email-action-title`, `--email-intro` (placeholders `{account}`, `{account_id}`, `{count}`), `--email-option` (repeatable), `--email-deadline` (placeholder `{date}`), and `--email-reply-by-days`. The action title, intro, each option, and the deadline accept Markdown (emphasis, links, lists, and line breaks). A single newline is a line break; a blank line starts a new paragraph. Raw HTML in those fields is shown as text. By default the email asks the owner to keep, delete, or request modifications and states **no deadline**; pass `--email-deadline` to add a reply-by sentence (`{date}` = report generation + `--email-reply-by-days`, default `14` days).

A Cost Explorer, inventory, or owner-tag failure on one account is recorded for that account and does not stop the rest of the run. OpenShift cluster lookup uses Dataverse Snowflake OAuth (`--snowflake-alias`, default `snowflake.account_alias`). An unknown or incomplete `--snowflake-alias` fails before AWS work. If the flag is omitted and no Snowflake account is configured, or the lookup fails after connect, AWS cost/inventory still succeed and the OpenShift section records a soft error (owner email shows a generic incomplete-lookup note).

Gmail uses existing gcloud Application Default Credentials. finops does **not** modify `~/.config/gcloud/application_default_credentials.json`. API quota is billed to `hcmfinops` via client options.

```bash
# Sign in with gcloud (only if needed; run this yourself)
# --disable-quota-project: do not write your gcloud config project into ADC
gcloud auth application-default login \
  --disable-quota-project \
  --scopes=https://www.googleapis.com/auth/cloud-platform,https://www.googleapis.com/auth/gmail.send,https://www.googleapis.com/auth/gmail.metadata

# Verify finops can send Gmail with your existing ADC
finops config gmail login

# Print account details (pretty-print)
finops account details --account-alias my-linked

# Machine-readable details
finops account details --payer rh-control --ou 'ou-abcd-12345678/*' --format json -o review.json

# OpenShift clusters from a specific Snowflake alias
finops account details --account-alias my-linked --snowflake-alias rhprod

# Test send to your inbox via plus-addressing (details still go to stdout)
finops account details --payer rh-control --account-id 111111111111 --send --redirect-prefix finops

# Production send to owner addresses
finops account details --payer rh-control --ou ou-abcd-12345678 --group-by owner --send --yes

# Production send with an explicit reply deadline (opt-in)
finops account details --payer rh-control --ou ou-abcd-12345678 --group-by owner --send --yes \
  --email-deadline 'If we do not hear from you by {date}, we will proceed with deletion.'

# Markdown in the intro, options, and deadline
finops account details --account-alias my-linked --send --redirect-prefix finops \
  --email-intro 'Please review **{account}**. See the [runbook](https://example.com).' \
  --email-option '**Keep** the account' \
  --email-deadline 'Reply by **{date}**.'
```

| Flag | Description |
|------|-------------|
| `--format` | Details output format: `pretty-print` (default), `json`, or `csv` |
| `--account-id` / `--account-alias` / `--ou` / `--tag` / `--payer` | Same account selection as `finops account get-cost` (`--ou`/`--payer` are ACTIVE members only; `--include-closed-accounts` is get-cost only) |
| `--output` / `-o` | Write details to this file instead of stdout |
| `--send` | Send owner notification emails via Gmail after printing details |
| `--yes` | With `--send`, deliver to resolved owner emails |
| `--redirect-prefix` | With `--send`, deliver to `PREFIX+<owner>@redhat.com` (test mode) |
| `--group-by` | `account` (default) or `owner` — one email per account or per owner (send only) |
| `--email-subject-prefix` | With `--send`, override the email subject prefix (plain text; default: `⚠️ Action required: `) |
| `--email-action-title` | With `--send`, override the `Action required` section heading (inline Markdown; raw HTML is shown as text) |
| `--email-intro` | With `--send`, override the email intro (Markdown; placeholders: `{account}`, `{account_id}`, `{count}`; raw HTML is shown as text) |
| `--email-option` | With `--send`, override a reply option (repeatable, inline Markdown; raw HTML is shown as text; default: Keep the account / Delete the account / Request modifications) |
| `--email-deadline` | With `--send`, add a deadline (Markdown; placeholder: `{date}`; raw HTML is shown as text); omitted means no deadline |
| `--email-reply-by-days` | With `--email-deadline`, days after report generation for `{date}` (default: `14`) |
| `--months` | Calendar months of cost history to include (default: `6`) |
| `--skip-regions` | Exclude regions from regional AWS inventory (comma-separated or repeatable); S3/Route53, costs, and OpenShift lookup remain account-wide |
| `--exclude-recent-days` | Omit the last N UTC days from the cost end anchor (incomplete AWS CE data); default from `defaults.cost.exclude_recent_days` or `0` |
| `--role` | Linked-account IAM role name. Overrides the alias's stored role when set; otherwise the alias role, then `defaults.aws.linked_role` |
| `--snowflake-alias` | Snowflake account alias for OpenShift cluster lookup (default: `snowflake.account_alias`) |
| `--quiet` | Suppress progress messages on stderr |
| `--workers` | Maximum concurrent workers for multi-account AWS queries (default: `25`, max: `1000`; use `1` for sequential) |

`--format json` on this command is account details (not a delivery summary). Delivery status with `--send` is printed on stderr.

JSON and CSV keep raw numbers (no thousands separators). CSV columns are `account_id`, `account_name`, `owner_email`, `section`, `id`, `name`, `attr1`, `attr2`, `attr3`, `attr4`, `attr5`, `amount`, `currency`, with one row per fact (`account`, `tag`, `month`, `top_service`, `ec2`, `rds`, `rds_cluster`, `route53`, `openshift`, `count`, `owner_error`, `inventory_error`, `openshift_error`).

When `inventory_error` is set, the AWS inventory scan was incomplete: JSON omits zero `resource_counts` fields, and CSV omits zero `count` rows. Check `inventory_error` before treating missing resources as confirmed unused. Pretty-print and owner email do the same for AWS inventory (no “None found” line for AWS types after a partial scan). Assume-role failures are recorded as inventory errors and still included in owner email with a generic incomplete-scan note. When `openshift_clusters_error` / `openshift_error` is set, OpenShift lookup was skipped or failed; owner email shows a generic incomplete-lookup note without raw Snowflake errors.

`--group-by`, `--redirect-prefix`, and the owner-email template flags (`--email-subject-prefix`, `--email-action-title`, `--email-intro`, `--email-option`, `--email-deadline`, `--email-reply-by-days`) require `--send`.

Gmail API quota/billing defaults to the `hcmfinops` GCP project (via API client options, not by changing ADC). Override with `FINOPS_GMAIL_QUOTA_PROJECT`.

### Region selection for resource scans

`finops account details`, `finops snapshot list`, and `finops extended-support list` accept `--skip-regions` to avoid regional resource discovery in one or more AWS regions. Supply a comma-separated list, repeat the flag, or combine both forms. Region names are trimmed and deduplicated. An explicitly empty list is rejected.

```bash
finops extended-support list --payer rh-control --skip-regions me-south-1,ap-east-1
finops snapshot list --payer rh-control --skip-regions me-south-1 --skip-regions ap-east-1
finops account details --account-alias my-linked --skip-regions me-south-1
```

By default, each account's enabled regions are discovered separately and exclusions are removed before regional scans start. When `--regions` is supplied on snapshot or extended-support commands, exclusions are removed from that explicit list instead. For example, `--regions us-east-1,us-west-2 --skip-regions us-west-2` scans only `us-east-1`. Excluding every selected region performs no regional resource calls and does not fall back to scanning all regions. Extended-support also applies exclusions to regional EKS catalog preloading and the skipped-account retry.

Exclusions apply to regional resource discovery. Authentication, enabled-region discovery, Cost Explorer, the global AWS Health lifecycle catalog, and account details' global S3/Route53 inventory and Snowflake OpenShift lookup still run. These calls can use endpoints in an excluded region. S3 buckets remain in account-wide inventory even when located in an excluded region. Snapshot output compares account-wide billed costs with costs for successfully scanned regions and uses the regional totals for attribution.

Pretty-print output identifies intentional exclusions separately from scan failures. JSON includes the normalized requested exclusions under `summary.excluded_regions` for snapshots and extended-support, and under `excluded_regions` on each account-details record. Account-details CSV includes `excluded_region` rows; snapshot and extended-support CSV remain findings-only. Account details and owner emails display the regional inventory scope, omit “None found” claims for AWS inventory, and omit zero resource-count fields/CSV rows when exclusions are present. Intentional exclusions do not become inventory errors or trigger account retries.

### Snapshot (AWS)

Find **EBS and RDS snapshots** older than a cutoff and estimate monthly storage cost. Account selection matches `finops account get-cost` (exactly one of `--account-id`/`--account-alias`, `--ou`, `--tag`, or `--payer` alone). Linked member accounts are scanned via role assumption from the payer.

```bash
finops snapshot list --account-alias rh-control
finops snapshot list --account-alias rh-control --older-than-days 365 --format json
finops snapshot list --payer rh-control
finops snapshot list --payer rh-control,osd-staging-1
finops snapshot list --payer rh-control --tag organization
finops snapshot list --ou ou-abcd-12345678 --payer rh-control --types ebs
finops snapshot list --account-id 333333333333 --payer rhc --older-than-days 90 --format csv
```

| Flag | Description |
|------|-------------|
| `--older-than-days` | List snapshots older than this many days (default: `365`) |
| `--types` | Snapshot types to scan: `ebs`, `rds`, or comma-separated (default: `ebs,rds`) |
| `--regions` | Limit scan to comma-separated AWS regions (default: all enabled regions) |
| `--skip-regions` | Exclude regions after selection (comma-separated or repeatable; exclusions override `--regions`) |
| `--min-size-gib` | Skip snapshots smaller than this size in GiB (default: `0`) |
| `--account-id` / `--account-alias` / `--ou` / `--tag` / `--payer` | Same account selection as `finops account get-cost` (`--ou`/`--payer` are ACTIVE members only; `--include-closed-accounts` is get-cost only) |
| `--role` | Linked-account IAM role name. Overrides the alias's stored role when set; otherwise the alias role, then `defaults.aws.linked_role` |
| `--format` | `pretty-print` (default), `json`, or `csv` |
| `--quiet` | Suppress progress messages on stderr |
| `--workers` | Maximum concurrent workers for multi-account AWS queries (default: `25`, max: `1000`; use `1` for sequential) |
| `--verbose` / `-v` | Log external commands and selected AWS API calls to stderr (see [AWS global flags](#aws-global-flags)) |

When Cost Explorer data is available, the summary compares **account-wide** billed EBS snapshot and RDS backup storage with billing for **successfully scanned regions**, including each regional total's percentage of account-wide spending. Both queries use the last complete calendar month. Region-filtered billing follows `--regions` and `--skip-regions` and excludes failed regions and skipped accounts from attribution. Accounts with the same completed region list are queried together; an empty region list returns zero regional costs without an unrestricted query.

**Attributed** storage costs for listed snapshots use the regional billed totals, scaled against run rates from the same scanned regions. Per-snapshot **$/MO** is each snapshot's share of that attributed cost; **—** on EBS means no incremental blocks. Where regional billing or matching run rates are unavailable, costs remain API estimates. RDS attribution also requires complete regional backup context, including scanned regions with no listed old snapshots.

JSON keeps account-wide totals in `summary.billed_costs`, adds the regional totals in `summary.regional_billed_costs`, and records each account's completed regions in `summary.scan_scopes`. JSON and CSV snapshot cost fields remain API estimates; attribution is applied in pretty-print output. Payer credentials need `ce:GetCostAndUsage` with `LINKED_ACCOUNT` and `REGION` filter access.

### Extended support (AWS)

Find AWS resources running versions in **AWS Extended Support**, with recent charges included in the account and service summaries. This covers **RDS DB instances, Aurora/RDS DB clusters, EKS Kubernetes clusters, ElastiCache caches, and OpenSearch Service domains**. Account selection matches `finops account get-cost` (exactly one of `--account-id`/`--account-alias`, `--ou`, `--tag`, or `--payer` alone). Linked member accounts are scanned via role assumption from the payer. RDS, EKS, ElastiCache, and OpenSearch are scanned by default.

```bash
finops extended-support list --account-alias rh-control
finops extended-support list --payer rh-control
finops extended-support list --payer rh-control --billing-lookback-days 7
finops extended-support list --payer rh-control --full-scan
finops extended-support list --payer rh-control --services rds --format json
finops extended-support list --payer rh-control --services eks --format json
finops extended-support list --payer rh-control --services elasticache --format json
finops extended-support list --payer rh-control --services opensearch --format json
finops extended-support list --payer rh-control --services rds,eks
finops extended-support list --payer rh-control --regions us-east-1,us-west-2
```

With `--payer` alone, the command first narrows the active member list using the payer's Cost Explorer data. It paginates `GetDimensionValues` for `USAGE_TYPE`, keeps values containing `extendedsupport` without regard to case, then uses those exact values in `GetCostAndUsage` filters grouped by `LINKED_ACCOUNT` and `USAGE_TYPE`. Values are combined in batches with OR semantics, reducing requests compared with querying each usage type separately. Accounts are retained regardless of whether their reported cost is positive, zero, or negative. Only matching active members proceed to role assumption and resource discovery. Explicit account, OU, and tag selections scan all their selected accounts without this billing filter.

The default billing lookback covers 30 UTC calendar days including today; `--billing-lookback-days` changes it. The same window is used for both Cost Explorer APIs, with an inclusive start and exclusive end. This selects accounts with recorded usage in that window, rather than proving which accounts currently have Extended Support resources. Recently upgraded or deleted resources can leave historical billing matches, and newly affected accounts may not appear yet: [Cost Explorer data can arrive later than 24 hours](https://docs.aws.amazon.com/cost-management/latest/userguide/ce-what-is.html). Resource APIs confirm the current findings. Use `--full-scan` with `--payer` to bypass billing selection and scan every active member.

Pretty-print output shows the billing window and selected/eligible account counts, even when no accounts match. JSON retains these details, matched usage types, the number of billed accounts, and a coverage notice under `summary.billing_selection`. Accounts excluded by billing selection are not reported as skipped accounts. A failed or incomplete Cost Explorer query stops discovery with an error; it never silently becomes an empty or partial candidate list. Payer credentials need `ce:GetDimensionValues` and `ce:GetCostAndUsage` for the default payer-wide selection. `--full-scan` bypasses this initial account-selection query; the automatic five-day check described below still needs billing permissions for scanned accounts with no current findings.

Pretty-print output also lists **Matched Extended Support usage types** and
**Accounts selected for scanning from Cost Explorer**. The account table shows
account ID, available name/alias, and one row per exact matched usage type.
These are the accounts selected before scanning, including accounts that later
have no findings or fail to scan. The full discovered usage-type filter remains
visible even when no eligible accounts are selected.

JSON stores the same account list in
`summary.billing_selection.selected_accounts`, with `account_id`, optional
`display_name`/`display_alias`, and `usage_types`. The existing
`summary.billing_selection.usage_types` contains the complete discovered filter.
Per-account types are deduplicated and sorted across pages, batches, months, and
queried payers. With multiple payers, each account's types are the union of its
matches across those payers. This evidence comes from the selection query itself;
no separate per-account billing lookup is required.

For comparisons with Cost Explorer, use the exact displayed billing window and
usage types. Selection is payer-wide and retains usage-type matches with zero or
negative recorded charges. It does not apply `--services`, `--regions`, or
`--skip-regions`; those flags limit the resource scan and 30-day cost summaries.
The five-day check ignores the service selection while honoring region filters.
Only eligible active member accounts proceed to scanning. `--full-scan` and
explicit account/OU/tag selection bypass this usage-based filter, so they do not
produce this audit list. Use pretty-print or JSON for the selection details;
CSV remains resource inventory only.

Each service uses its own APIs to discover the running version and retrieve support lifecycle dates:

| Service | Resource version | Support lifecycle |
|---------|------------------|-------------------|
| RDS / Aurora | `DescribeDBInstances` / `DescribeDBClusters`, then `DescribeDBEngineVersions` resolves the major version | `DescribeDBMajorEngineVersions` |
| EKS | `DescribeCluster` returns the Kubernetes version | `DescribeClusterVersions` |
| ElastiCache | `DescribeCacheClusters` / `DescribeServerlessCaches` | AWS Health `DescribeServiceLifecycle` |
| OpenSearch | `DescribeDomain` returns the engine/version | AWS Health `DescribeServiceLifecycle` |

The `major_engine_version` field in JSON and CSV uses service-specific version granularity. Interpret it together with `service` and `engine`:

| Service | Meaning of `major_engine_version` | Example |
|---------|-----------------------------------|---------|
| RDS / Aurora | `MajorEngineVersion` returned by `DescribeDBEngineVersions` | `15` for PostgreSQL; `8.0` for Aurora MySQL |
| EKS | Kubernetes major.minor version, identical to `engine_version` | `1.28` |
| ElastiCache | First dot-separated segment of `engine_version` | `7` for Redis `7.1` |
| OpenSearch | Numeric major.minor version, identical to `engine_version` | `2.11` |

An RDS resource is reported when the standard support end date for its engine major version (from `rds:DescribeDBMajorEngineVersions`) is in the past. The output includes both the standard support end date and, when published, the extended support end date. Resources at or past a published extended support end date appear separately under "Past extended support". Supported engines are MySQL, PostgreSQL, Aurora MySQL, and Aurora PostgreSQL. Version lookup includes deprecated versions, and linked-account roles are assumed when each account's scan begins.

For EKS, the run first preloads `DescribeClusterVersions` catalogs for the payer's enabled regions (or `--regions`), including all versions and pagination. Successful regional catalogs are shared across accounts and the final retry pass. If a payer cannot load a catalog, a member role can load it during scanning; additional member-enabled regions are loaded on demand. Failed catalog requests are retained per credential source, so one denied account does not prevent another account from loading the catalog. Catalogs are kept in memory for the current command only.

EKS clusters are discovered with `ListClusters` and `DescribeCluster`. Each cluster's Kubernetes version is matched against its region's catalog. A cluster is reported when the scan timestamp is at or after standard support ends and before extended support ends. An `EXTENDED` upgrade policy alone does not qualify a cluster. Unknown versions, missing support dates, and failed cluster descriptions produce resource warnings. Versions at or past their extended support end date appear under "Past extended support". An `UNSUPPORTED` catalog status without a passed extended support end date remains unknown. EKS records use service `eks`, resource type `eks-cluster`, engine `kubernetes`, and the Kubernetes version in the version fields; `engine_lifecycle_support` holds the cluster's upgrade policy when available. EKS Connector external clusters are excluded.

ElastiCache discovery paginates `DescribeCacheClusters` and `DescribeServerlessCaches`. Node-based cache clusters, including replication-group members, are checked individually using their current engine version. Each is reported once as `elasticache-cluster`; replication groups are not counted again. Serverless caches use resource type `elasticache-serverless-cache`, with the full engine version when available and the major version as a fallback. Tags are fetched only for confirmed findings using `ListTagsForResource`; failed tag lookups retain the resource and produce a warning.

ElastiCache support schedules come from the AWS Health version catalog, matched by engine and version. Redis OSS, Valkey, and Memcached are evaluated using the catalog; eligibility is not determined by a built-in engine/version list. Lookup uses the most specific usable entry: exact version, then major.minor, then major version. If an entry has missing, ambiguous, or inconsistent lifecycle information, or no milestones applicable to the resource's region, lookup tries the next broader version.

Each candidate is evaluated independently; dates from different version entries are not combined. An entry that establishes the version is still in standard support, has not yet reached an explicit Extended Support start, or is past Extended Support takes precedence over broader schedules. If no candidate can determine the current support status, the resource remains unclassified and its warning lists the failed version entries and reasons.

For ElastiCache and OpenSearch, a version entry containing only applicable `supported` lifecycle events is treated as supported without requiring cutoff dates. Those resources appear in neither the Extended Support findings nor "Support status unknown". Event types are matched without regard to case, and events must apply to the resource's region (or `all`). A supported-only entry takes precedence over broader version schedules. When other applicable events exist, the lifecycle dates and end-of-life rules determine classification; `supported` does not suppress incomplete or conflicting lifecycle information.

OpenSearch Service discovery uses `ListDomainNames` (an unpaginated API) and `DescribeDomain` for each unique domain, including both OpenSearch and legacy Elasticsearch. Deleted domains are excluded. Findings use service `opensearch`, resource type `opensearch-domain`, and engine `opensearch` or `elasticsearch`. The numeric major.minor version is stored in both version fields because lifecycle schedules apply at that level. Tags are fetched only for confirmed findings using `ListTags`; failed tag lookups retain the domain and produce a warning. A failed domain description does not stop other domains from being checked. OpenSearch Serverless collections are excluded because AWS manages their engine versions.

Health catalog service `ES` contains both Elasticsearch and OpenSearch entries. Version `es/7.10` is normalized to engine `elasticsearch` and version `7.10`, matching `Elasticsearch_7.10` domains. Version `os/2.11` is normalized to engine `opensearch` and version `2.11`, matching `OpenSearch_2.11` domains. Both use service `opensearch` internally. The `es` and `os` engine prefixes are accepted without regard to case with `/`, `_`, or `:` separators. This normalization happens when loading the catalog, before lifecycle matching and lookup caching.

For ElastiCache and OpenSearch, the run first fetches all pages of [`health:DescribeServiceLifecycle`](https://docs.aws.amazon.com/health/latest/APIReference/API_DescribeServiceLifecycle.html), retaining metadata for those services. It uses payer credentials for linked-account scans, or the account's own credentials for direct scans. The successful catalog is cached in memory and shared across accounts, regions, and the skipped-account retry. Another configured credential source can load it if the first fails; members do not each repeat a failed payer Health query. A final skipped-account retry permits failed catalog loads to be attempted again with fresh credential state. RDS-only and EKS-only scans do not call AWS Health.

The Health catalog supplies service/engine/version identifiers, lifecycle milestones, dates, and applicable regions. Both `standard_support_end` and `extended_support_end` are recognized without regard to case. Classification uses the API timestamps directly, with no added calendar-day offset: a resource qualifies from the standard-support end boundary (or explicit Extended Support start boundary) until the Extended Support end boundary, which is exclusive. Before the standard-support end it is omitted; at the extended-support end it moves to "Past extended support". A `supported` event alongside these dated milestones does not override their dates. Explicit region-specific milestones override global milestones. Resources remain unclassified when applicable version entries cannot supply usable lifecycle dates and neither supported-only events nor a reached end-of-life event establish their status. This includes unrecognized events, missing dates, or conflicting milestones. No support-date tables are bundled or downloaded from documentation.

Lifecycle lookup uses one pass through each candidate's events. Results from the loaded catalog, including unknown matches, are cached by service, engine, resource version, region, and scan timestamp. Resources sharing those values reuse the result across accounts and the skipped-account retry. Different regions and timestamps resolve independently, and lookups made before the catalog is available are not cached.

The catalog credential source needs `health:DescribeServiceLifecycle` and an [AWS Support plan eligible for Health API access](https://docs.aws.amazon.com/health/latest/ug/health-api.html). The commercial Health endpoint is queried in `us-east-1`; each event's region list determines where it applies. API permission, subscription, or incomplete pagination failures produce `summary.lifecycle_warnings` in JSON and warnings in pretty output and on stderr (including with CSV and `--quiet`). Affected resources appear under `summary.unclassified_resources`; other discoveries, including RDS/EKS findings, remain available. CSV continues to contain only confirmed findings. There is no fallback to embedded schedules.

Each run shares validated payer configuration and an STS client across the payer's linked accounts. Member roles are still assumed and their identities verified separately. If role assumption reports invalid or expired payer credentials, one worker reloads the payer profile and, for SAML authentication, reruns the existing login flow if needed; other workers wait for that renewal. The affected role assumption is retried once. Permission errors do not trigger login. Failed renewal is shared across dependent accounts so login is not repeated for every member. SAML renewal requires working VPN/Kerberos access. With `--auth-method profile`, the configured SDK provider/profile is reloaded; scan workers never prompt for access keys, and profiles that require an external login must be renewed externally.

| Flag | Description |
|------|-------------|
| `--services` | Services to scan: `rds`, `eks`, `elasticache`, `opensearch` (comma-separated; default `rds,eks,elasticache,opensearch`) |
| `--regions` | Limit scan to comma-separated AWS regions (default: all enabled regions) |
| `--skip-regions` | Exclude regions from resource scans and EKS catalog preloading (comma-separated or repeatable; exclusions override `--regions`) |
| `--account-id` / `--account-alias` / `--ou` / `--tag` / `--payer` | Same selector forms as `finops account get-cost`; `--ou`/`--payer` are ACTIVE members only. `--payer` alone additionally applies the Cost Explorer candidate filter |
| `--full-scan` | With `--payer` alone, bypass Cost Explorer and scan every active member |
| `--billing-lookback-days` | UTC calendar days including today used for payer-wide billing selection (default: `30`; minimum: `1`) |
| `--role` | Linked-account IAM role name. Overrides the alias's stored role when set; otherwise the alias role, then `defaults.aws.linked_role` |
| `--format` | `pretty-print` (default), `json`, or `csv` |
| `--output` / `-o` | Write output to a file instead of stdout |
| `--quiet` | Suppress progress messages on stderr |
| `--workers` | Maximum concurrent workers for multi-account AWS scans (default: `25`, max: `1000`; use `1` for sequential) |
| `--verbose` / `-v` | Log external commands and selected AWS API calls to stderr (see [AWS global flags](#aws-global-flags)) |

Required IAM permissions in each scanned account: `ec2:DescribeRegions`; for RDS, `rds:DescribeDBInstances`, `rds:DescribeDBClusters`, `rds:DescribeDBEngineVersions`, and `rds:DescribeDBMajorEngineVersions`; for EKS, `eks:ListClusters`, `eks:DescribeCluster`, and `eks:DescribeClusterVersions`; for ElastiCache, `elasticache:DescribeCacheClusters`, `elasticache:DescribeServerlessCaches`, and `elasticache:ListTagsForResource`; for OpenSearch Service, `es:ListDomainNames`, `es:DescribeDomain`, and `es:ListTags`. Failed tag lookups produce warnings but retain findings. Payer credentials also need `sts:AssumeRole` into the configured linked-account role. Allow `eks:DescribeClusterVersions` on the payer for catalog preloading. `eks:ListClusters` and `eks:DescribeClusterVersions` require resource `"*"`; `eks:DescribeCluster` can be scoped to cluster ARNs. Use `--services rds`, `--services eks`, `--services elasticache`, or `--services opensearch` to limit the scan to one service.

After the initial scan finishes, accounts skipped because credentials could not be loaded or enabled regions could not be discovered are retried once. The retry uses fresh payer-session state, reloads credentials, and can renew a SAML login if needed. Successful accounts are not rescanned. Recovered findings are merged into the result and totals; only accounts that still fail remain under "Skipped accounts" (`summary.skipped_accounts` in JSON), with their latest failure reason. This list is held in memory for the run and can be saved with JSON output. Region-only failures and accounts excluded during account selection are not included in this retry.

Failed service discovery is listed under "Skipped regions", including partial discovery failures. Warnings include the affected service in pretty-print and JSON output. A failure in one service does not prevent the other services from being scanned or discard their findings. Successful discoveries from the same region are retained. An engine/version lookup failure affects only the resources that depend on that lookup. Region/service failures and resource warnings are not retried by the final skipped-account retry pass.

Resources at or past a known extended support end date appear under "Past extended support" in pretty-print output, with a separate count, resource identifiers, versions, and support dates. JSON retains these records under `summary.past_extended_support_resources`. This applies to RDS, EKS, ElastiCache, and OpenSearch when their lifecycle data supplies the end date. These resources are excluded from the current Extended Support count, its service/account totals, and "Support status unknown". The end boundary is exclusive for current Extended Support, so a resource enters "Past extended support" exactly at that timestamp.

For ElastiCache and OpenSearch, a dated AWS Health `end_of_life` event also puts the resource under "Past extended support" as soon as that timestamp is reached in the applicable region. End of life establishes this terminal status independently of missing or unusable standard/extended support milestones. The event type is matched without regard to case; regional overrides still apply, and missing or conflicting end-of-life dates cannot establish this status. A future end-of-life event alone does not establish the current support status. The end-of-life date appears in a separate "END OF LIFE" column in pretty-print output and as `end_of_life_date` in JSON and CSV. The CSV column uses `YYYY-MM-DD` when available and is blank otherwise. Unavailable support dates are shown as "—" and omitted from JSON rather than inferred from the end-of-life date.

Resources with missing engine/version information, failed lifecycle lookups, or missing standard support end dates appear under "Support status unknown" in pretty-print output and `summary.unclassified_resources` in JSON. They are excluded from the Extended Support count. Resources still in standard support or before an explicit Extended Support start appear in neither section. CSV contains only resources currently in Extended Support; use JSON to export past-support records and unknown resources. Check warnings before treating an empty result as "no resources in extended support".

#### Recent charges without discovered Extended Support resources

After scanning and the final skipped-account retry, the command automatically
checks accounts with **no current Extended Support findings** for charges during
**the last five complete UTC days through yesterday**. This runs independently
of the 30-day cost summaries and the account-selection billing lookback. It reuses the shared
period resolver with `Days: 5` and no recent-day exclusion. The latest two days may
be incomplete.

Accounts with a positive `NetUnblendedCost` total appear in a separate
**Recent charges with unknown Extended Support resources** section, showing
account ID, alias when available, the five-day charge amount, and whether scan
warnings apply. This section remains visible even when no resources were found
anywhere. It can identify gaps in resource discovery, but charges can also reflect
recently upgraded or deleted resources and do not prove a resource is still in
Extended Support.

Accounts that remain entirely skipped after retries are excluded and stay under
**Skipped accounts**. Accounts with partial regional/service scans or unknown
support status can appear, marked **See scan warnings**. Accounts with only
past-support records also qualify because those records do not establish a
current finding. Accounts excluded by initial billing selection are not queried,
since they were not scanned.

The check covers **all services**, ignoring `--services` and the list of implemented
discovery services. It discovers every usage type containing `extendedsupport` and
queries those exact values without any `SERVICE` filter, so unimplemented services
can appear. It still honors `--regions`/`--skip-regions` and uses payer or
direct-account billing credentials with ordinary `GetDimensionValues`/
`GetCostAndUsage` queries grouped by account. Credentials need
`ce:GetDimensionValues` and `ce:GetCostAndUsage` whenever this check runs, including
with explicit account selection or `--full-scan`. No resource IDs are used. Zero
or negative net charges are omitted. Failed billing sources produce warnings;
partial results from a failed source are never presented as complete charges.

JSON stores the five-day period, scope, warnings, and charged accounts under
`summary.charges_without_findings`, with `all_services: true` and an empty `services`
list to indicate that no service restriction applies. These accounts are separate
from resource counts and the 30-day summaries. CSV remains resource inventory only;
use pretty-print or JSON to view this additional report.

#### Account and service charges in the resource list

`extended-support list` automatically includes Extended Support charges beside
the resource counts in the **By account** and **By service** sections:

```bash
finops extended-support list --payer rh-control
finops extended-support list --payer rh-control --services rds,eks
finops extended-support list --payer rh-control --format json -o support-summary.json
```

The snapshot covers the **last 30 complete UTC days through yesterday**, using
the shared period resolver with `Days: 30` and no recent-day exclusion. It includes
the latest two complete days, which may be incomplete. The report shows the exact
dates and cost basis (`NetUnblendedCost`). This fixed window is independent of
`--billing-lookback-days`, which selects accounts for scanning.

Costs are fetched for accounts with current findings, using the selected services
and the `--regions`/`--skip-regions` billing scope. The counts describe current
resources; the historical account charges can include resources upgraded or
deleted during the window. A service with historical charges and no remaining
current findings appears with a count of zero. These amounts are account/service
charges and are not attributed or divided among individual resources.

The implementation reuses `extended-support get-cost` billing queries. It discovers
usage types containing `extendedsupport` without regard to case, filters by their
exact values and the selected services/accounts/regions, and groups ordinary
`GetCostAndUsage` results by linked account. It sums the time buckets for the
account and service summaries. Payer credentials query linked-account charges;
member discovery roles are not used for billing. Explicit account scoping also
applies when the payer account itself is scanned. Billing credentials need only
`ce:GetDimensionValues` and `ce:GetCostAndUsage`; no resource-level billing settings
or resource IDs are required.

Billing failures preserve resource inventory, show `Unavailable` for affected
accounts, and produce warnings. Service totals are unavailable if any contributing
account is unavailable, rather than displaying partial totals as complete. Service
totals also remain unavailable when accounts use different currencies. A successful
query with no matching charges returns zero. Missing metrics never silently change
the cost basis. The feature does not display estimated or avoidable-cost columns.

JSON stores charges in `summary.by_account[].support_cost` and
`summary.by_service[].support_cost`. `summary.costs` records the reporting window,
metric, account/service/region scope, and warnings. Resource detail rows and CSV
contain inventory only; use JSON to export cost summaries.

#### Extended support costs

`finops extended-support get-cost` queries historical Extended Support charges directly
from Cost Explorer. It does not scan resources or assume roles into member accounts.

```bash
finops extended-support get-cost --payer rh-control
finops extended-support get-cost --payer rh-control --services rds,elasticache
finops extended-support get-cost --payer rh-control --regions us-east-1,us-west-2 --skip-regions us-west-2
finops extended-support get-cost --payer rh-control --group-by service,account
finops extended-support get-cost --payer rh-control --from 2026-08-01 --to 2026-10-31
finops extended-support get-cost --payer rh-control --days 180 --granularity daily
finops extended-support get-cost --account-alias my-account --format json
finops extended-support get-cost --payer rh-control --format csv -o support-costs.csv
```

| Option | Behavior |
|--------|----------|
| `--services` | Defaults to all supported services. Only `rds`, `eks`, `elasticache`, and `opensearch` are accepted (comma-separated). |
| `--regions` | Optional comma-separated AWS region codes. Limits billing to those regions; defaults to all billing regions. |
| `--skip-regions` | Optional comma-separated or repeatable exclusions; takes precedence over `--regions`. Excluding every explicitly selected region produces an empty report, never an unrestricted query. |
| `--group-by` | Optional; defaults to `service`. Also accepts `account` and `service,account`. |
| `--from`, `--to` | Optional inclusive UTC dates, using the shared cost-period resolver. `--from` alone ends at the buffered cutoff; `--to` requires `--from` and cannot exceed that cutoff. |
| `--days` | Defaults to **90** days ending at the buffered cutoff; mutually exclusive with `--months` and explicit dates. |
| `--months` | Optional calendar-month window, using the same semantics as `account get-cost`; mutually exclusive with `--days` and explicit dates. |
| `--exclude-recent-days` | Defaults to **2**, including with explicit period options. Use `0` to include recent complete UTC days through yesterday. |
| `--granularity` | Optional `daily` or `monthly`. Defaults to **one period total per service**, including with explicit `--days`, `--months`, or dates. Specifying granularity requests that time breakdown alongside full-period service totals. |
| `--metric` | Defaults to `NetUnblendedCost` for after-discount charges. `UnblendedCost` explicitly requests gross charges. Missing metrics fail the report rather than silently switching cost basis. |
| `--format`, `--output` | Pretty-print, JSON, or CSV; optional `--output`/`-o` file. |

For example, on November 5 UTC, the default period is August 5–November 2 inclusive
(90 days). November 3 and 4 are omitted from the previous 92 complete days, and
today is not included. This buffer gives recent billing data time to update;
Cost Explorer estimated-data flags and billing-delay notices remain visible.

Without `--granularity`, the report combines the entire selected window into one
total for each service. For example, `--from 2026-08-01 --to 2026-10-31` returns
one total per service for August through October. When no period options are
specified, a note explains that the report defaults to the last 90 reliable
Cost Explorer days. An explicit `--group-by` still controls the grouping of these
totals. JSON identifies this view with
`granularity: "total"`; JSON/CSV row dates cover the full reporting window, and
the pretty-print table omits repeated date columns. Region/service/account filters
continue to apply. Use `--granularity daily` or `--granularity monthly` to request
time buckets for any period.

When granularity is requested, pretty-print output shows **Totals by service for
full period** above the time breakdown, including with `--group-by account` or
`--group-by service,account`. These totals combine all selected accounts and time
buckets for each service using the same billing responses, with no extra AWS
queries. JSON includes these full-period records under `service_totals`, with
inclusive dates, cost amounts, and estimated-data flags; `rows` retains the
requested breakdown. CSV contains only the breakdown rows so summing its amounts
does not count the service totals again.

Pretty-print output also shows **Period coverage**, including in totals-only
reports. It counts days with matching records for daily requests, or months for
monthly and totals-only requests. Periods with no matching records are listed
as **No matching charges returned** when a period was returned with empty groups,
or **No period returned** when every cost query omitted it. If only some cost
queries returned a period, it is marked **some period responses missing**, even
when other responses contain charges for that period. Pages of each query are
combined before making this assessment. When no matching Extended Support usage
types are discovered, coverage says **No matching usage types; costs not queried**.
No extra billing requests are made for coverage.

These statuses describe the filtered API responses; they cannot distinguish no
usage from unavailable history. Totals include only returned billing records.
Explicit zero-cost records remain zero-valued cost rows and count as matching
records; zero and negative amounts do not imply a coverage gap. JSON retains
every expected period with inclusive dates, status, and an `incomplete` flag
where applicable under `coverage`, alongside `total_periods`,
`periods_with_records`, and the coverage granularity. CSV contains cost rows
only; use pretty-print or JSON to inspect period coverage. An empty account or
explicit region scope that bypasses billing does not report coverage.

Period handling reuses the flags, validation, and `core/cost.ResolvePeriod` logic
used by `account get-cost` and AWS reports. With no period options, this command
acts as `--days 90 --exclude-recent-days 2`; `--months` or explicit dates replace
the default rolling-days mode. These command-specific defaults remain independent
of `defaults.cost.*`. An explicit `--to` inside the excluded recent days is rejected
rather than silently shortened.

With `--payer` alone, billing scope is the entire payer bill, including the payer
account and historical charges from members that have since closed. Multiple payer
aliases are supported. Explicit `--account-id`/`--account-alias` selections restrict
costs to those accounts, including when an explicit account is the payer itself.
`--ou` and `--tag` use current Organizations membership/tags; they cannot reconstruct
historical membership. No active-member or current-resource filter is applied to
payer-wide billing. Credentials need `ce:GetDimensionValues` and `ce:GetCostAndUsage`;
OU/tag selection additionally needs the corresponding Organizations permissions.

Region flags filter Cost Explorer's `REGION` billing dimension directly; there is
no enabled-region discovery. With only `--skip-regions`, all billing regions except
the exclusions remain in scope, including costs without a regional assignment.
Usage-type discovery is a global metadata lookup; every cost query applies the
region filter, including all paginated requests and service/account batches.
Requested and excluded regions are retained in pretty output, JSON (`regions`,
`excluded_regions`), and CSV columns of the same names.

The report discovers usage types containing `extendedsupport` without regard to
case over the requested period, then filters by exact usage types and billing
service names. Both current and historical billing names for EKS and OpenSearch
are recognized. Requests are paginated and usage filters are batched. A failed
or malformed billing response fails the report without returning partial totals;
zero and negative charges are retained, and different currencies are never summed.
Daily/monthly rows show inclusive dates and retain Cost Explorer's `estimated` flag.
Only time buckets with matching account groups are returned; an empty report means
no matching charges were recorded, not that no resources exist in Extended Support.

Recorded charges are **not total resource running costs**. `amount` is billing
associated with Extended Support usage types. `avoidable_amount` identifies
historical support surcharges billed separately from base usage for RDS,
ElastiCache, and OpenSearch. These charges stop after an eligible upgrade, but
their historical amount is not a forecast of future net savings, which can also
depend on changed usage, pricing, discounts, and upgrade costs. The classification
follows AWS's [RDS charge documentation](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/extended-support-charges.html),
[ElastiCache charge documentation](https://docs.aws.amazon.com/AmazonElastiCache/latest/dg/extended-support-charges.html),
and [OpenSearch charge documentation](https://docs.aws.amazon.com/opensearch-service/latest/developerguide/what-is.html).

EKS Extended Support billing can include the ordinary control-plane fee:
[AWS currently describes its extended-support price as standard support plus an additional fee](https://aws.amazon.com/eks/pricing/).
Without a verified applicable baseline, this command reports EKS avoidable amounts
as **unavailable** instead of applying a fixed multiplier to billed charges. Any
aggregate containing EKS billing also has an unavailable complete avoidable amount;
`known_avoidable_amount` remains a clearly incomplete subtotal of identifiable
surcharges. Unknown amounts are JSON `null`, blank in CSV, and `unavailable` in
pretty output. JSON/CSV include notices describing this cost basis and limitations.
No live pricing lookup, per-resource attribution, or full-resource cost estimate
is performed by this command.

### Reports

Generate HTML reports from configured accounts. Templates use Go's **`html/template`**, embedded in the CLI binary under `cli/internal/report/templates/`.

```bash
finops report list
finops report create costs --account-alias rh-control
finops report create costs --account-alias rh-control -o costs.html
finops report create costs --account-id 333333333333 --payer rhc -o member.html
finops report create costs --ou ou-abcd-12345678 --payer rh-control -o ou-costs.html
finops report create costs --payer rh-control -o all-members.html
finops report create costs --payer rh-control --tag env=prod -o prod.html
```

The **costs** template includes:

- Total net amortized cost for the selected period (same flags and config defaults as `account get-cost`)
- Breakdown by linked AWS account
- Breakdown by organizational unit (when org structure can be resolved)
- Breakdown by AWS service
- Daily cost trend chart (embedded SVG; works when opening the HTML file locally)

| Flag | Description |
|------|-------------|
| `template` | Positional argument: report template name (run `finops report list` for options) |
| `--format` | Output format (default: `html`) |
| `--account-id` / `--account-alias` / `--ou` / `--tag` / `--payer` | Same account selection as `finops account get-cost` (exactly one mode; `--ou`/`--payer` are ACTIVE members only; `--include-closed-accounts` is get-cost only) |
| `--skip-org-cache` | Bypass cached organization account/tag data for `--tag` selection |
| `--refresh-org-cache` | Refresh organization cache from AWS |
| `--verbose` / `-v` | Log external commands and selected AWS API calls to stderr (see [AWS global flags](#aws-global-flags)) |
| `--output` / `-o` | Write HTML to a file instead of stdout |
| `--quiet` | Suppress progress messages on stderr (HTML still goes to stdout or `--output`) |
| `--workers` | Maximum concurrent workers for multi-account AWS queries (default: `25`, max: `1000`; use `1` for sequential; **costs** template only) |
| `--days`, `--months`, `--from`, `--to`, `--exclude-recent-days` | Same period options as `finops account get-cost` |

Progress lines (tag resolution, credential checks, Cost Explorer queries) are printed to **stderr** so you can redirect output safely, e.g. `finops account get-cost ... --format json > costs.json`.

Use `--quiet` to suppress progress messages.

Tag-based account selection caches organization account and tag listings via the shared finops cache service (`cli/internal/cache`) under `cache/org/<payer-account-id>.json` next to your config (default TTL: 1 hour). Use `--refresh-org-cache` to force a refresh or `--skip-org-cache` to always query AWS live. These cache flags apply to `--tag` selection only.

When many linked accounts under the same payer are queried together (typical for `--payer` alone, `--tag`, or `--ou`), `finops account get-cost` uses **bulk Cost Explorer queries** grouped by linked account instead of one API call per account (usually a single CE call for up to 100 linked accounts; use `--workers` to parallelize batched CE calls when there are more than 100 accounts). `finops snapshot list` and `finops report create costs` use `--workers` (default `25`, max `1000`) to parallelize per-account scans, assume-role setup, and batched CE queries when selections are large.

## Cross-compile (local)

```bash
GOOS=linux GOARCH=amd64 go build -o bin/finops-linux-amd64 ./cli/cmd/finops
GOOS=windows GOARCH=amd64 go build -o bin/finops.exe ./cli/cmd/finops
GOOS=darwin GOARCH=arm64 go build -o bin/finops-darwin-arm64 ./cli/cmd/finops
```

## Releases

Releases are built with [GoReleaser](https://goreleaser.com/) on tag push (`v*`). Artifacts include **linux**, **darwin**, and **windows** (amd64 and arm64 where applicable).

1. Merge changes to the default branch.
2. Create and push a tag, e.g. `git tag v0.1.0 && git push origin v0.1.0`.
3. The [Release workflow](.github/workflows/release.yml) publishes binaries to GitHub Releases.

Download a release asset, extract it, and run:

```bash
finops --help
```

## CI

- **Test** (`.github/workflows/test.yml`): runs on pull requests and pushes to `main`/`master` (tests and builds CLI + API).
- **Release** (`.github/workflows/release.yml`): runs on version tags.
