# bots-platform

Lightweight multi-bot Telegram platform for a small VPS: one Go binary, one Docker image, one backend container, one webhook ingress, and many bot manifests.

## Architecture

- Runtime shape: modular monolith in Go with one HTTP server and optional in-memory worker loop in the same process.
- Webhooks: `POST /telegram/{bot_slug}/webhook`
- Health: `GET /healthz`
- Routing: bot slug resolves to an in-memory runtime entry built from `config/bots.yml`, `registry/bots.lock.json`, and vendored `registry/bots/<slug>/bot.yaml`
- Shared infra: one Telegram client layer, one shared `http.Client`, optional shared Redis client, optional shared Postgres client, and one lightweight job runner
- Deployment model: Telegram bots are separate content repos, but all execution stays inside this single image and single backend container

## Responsibility Split

### `bots-platform`

- owns executable logic, module registry, HTTP runtime, shared clients, workers, Docker, deploy, and registry sync
- adds new bot behavior by implementing a new module under `internal/bots/modules/...`
- vendors pinned bot snapshots into `registry/bots/<slug>`

### bot repo

- owns `bot.yaml`, texts, templates, assets, content tests, and dispatch workflow
- never ships its own container or standalone service
- picks one existing module via `bot.yaml.module`
- keeps secrets out of Git; only references env var names

## Project Tree

```text
bots-platform/
  cmd/server/main.go
  cmd/botsctl/main.go
  config/bots.yml
  config/bots.example.yml
  internal/app/app.go
  internal/bots/
  internal/config/config.go
  internal/httpserver/server.go
  internal/infra/
  internal/jobs/runner.go
  internal/logging/logging.go
  internal/registry/
  internal/telegram/
  registry/bots.lock.json
  registry/bots/sample-echo/bot.yaml
  registry/schemas/bot.schema.json
  examples/bot-repo/
  .github/workflows/deploy.yml
  Dockerfile
  docker-compose.yml
  .env.example
```

## Runtime MVP

- `echo` module:
  - `/start` sends the bot-specific `welcome_message`
  - other text sends `Echo: <text>`
  - non-text updates are ignored
- Secrets are resolved at startup from env names declared in each manifest.
- Redis and Postgres are lazy extension points. If `REDIS_URL` or `POSTGRES_URL` are empty, the app still boots.
- Worker runner supports `server`, `worker`, and `all` modes. On the target VPS, use `APP_MODE=all`.

## `bot.yaml` Schema

Required fields:

- `slug`
- `name`
- `description`
- `enabled`
- `module`
- `welcome_message`
- `telegram.token_env`
- `telegram.webhook_secret_env`

Optional fields:

- `features`
- `jobs`
- `integrations`
- `env_schema`

See [registry/schemas/bot.schema.json](/D:/claude/боты_портфолио/registry/schemas/bot.schema.json) and [registry/bots/sample-echo/bot.yaml](/D:/claude/боты_портфолио/registry/bots/sample-echo/bot.yaml).

## Config Files

### `config/bots.yml`

Human-maintained catalog of allowed bots:

```yaml
bots:
  - slug: sample-echo
    repo: your-org/sample-echo-bot
    default_ref: main
    snapshot_path: .
```

### `registry/bots.lock.json`

Machine-maintained pin file:

```json
{
  "version": 1,
  "bots": [
    {
      "slug": "sample-echo",
      "repo": "your-org/sample-echo-bot",
      "ref": "main",
      "sha": "1111111111111111111111111111111111111111",
      "snapshot_path": ".",
      "synced_at": "2026-04-30T00:00:00Z"
    }
  ]
}
```

## Local Run

1. Copy [.env.example](/D:/claude/боты_портфолио/.env.example) to `.env`.
2. Fill `BOT_SAMPLE_ECHO_TOKEN` and `BOT_SAMPLE_ECHO_WEBHOOK_SECRET`.
3. Run the server:

```bash
go run ./cmd/server -mode=all
```

Health check:

```bash
curl http://localhost:8080/healthz
```

Validate catalog and vendored snapshots:

```bash
go run ./cmd/botsctl validate
```

## Run with Docker Compose

```bash
docker compose up -d --build
```

Health check:

```bash
curl http://localhost:8080/healthz
```

## Telegram Webhook Setup

```bash
curl -X POST "https://api.telegram.org/bot${BOT_SAMPLE_ECHO_TOKEN}/setWebhook" \
  -d "url=https://bots.example.com/telegram/sample-echo/webhook" \
  -d "secret_token=${BOT_SAMPLE_ECHO_WEBHOOK_SECRET}"
```

## Add a New Bot

1. Copy [examples/bot-repo](/D:/claude/боты_портфолио/examples/bot-repo) into a new GitHub repository.
2. Edit `bot.yaml`:
   - choose a unique `slug`
   - point `module` to an existing runtime module such as `echo`
   - update `telegram.*_env` names
3. Add repository secrets:
   - `BOTS_PLATFORM_DISPATCH_TOKEN`
   - `BOTS_PLATFORM_OWNER`
   - `BOTS_PLATFORM_REPO`
4. In `bots-platform`, register the bot in [config/bots.yml](/D:/claude/боты_портфолио/config/bots.yml).
5. On the VPS or deploy target, add the env vars referenced by `telegram.token_env` and `telegram.webhook_secret_env`.
6. Push the bot repo to `main`. Its workflow validates `bot.yaml`, runs smoke tests, and sends `repository_dispatch` to `bots-platform`.

## Add a New Bot Module

1. Create a new package under `internal/bots/modules/<module-name>/`.
2. Implement:

```go
type Module interface {
    Name() string
    HandleUpdate(ctx context.Context, bot *BotRuntime, update telegram.Update) error
}
```

3. Register the module in `internal/app/app.go` when building `bots.NewModuleRegistry(...)`.
4. Update bot repos to reference the new `module` value in `bot.yaml`.
5. Add focused tests for the new behavior before deploying.

## Dispatch, Lock, and Single Deploy Flow

1. A bot repo push to `main` validates its manifest and fires `repository_dispatch` with:
   - `bot_slug`
   - `repo`
   - `ref`
   - `sha`
2. `.github/workflows/deploy.yml` in `bots-platform` checks out that pinned SHA, validates `bot.yaml`, and runs:

```bash
go run ./cmd/botsctl sync-bot \
  -slug sample-echo \
  -repo your-org/sample-echo-bot \
  -ref main \
  -sha <pinned-sha> \
  -source .dispatch-bot
```

3. The workflow updates `registry/bots/<slug>/...` and `registry/bots.lock.json`, commits the snapshot pin, builds one Docker image, pushes it to GHCR, and deploys one backend service on the VPS.
4. The VPS updates through:

```bash
docker compose pull && docker compose up -d
```

This keeps rollback simple: redeploy any previous image tag or revert the registry/runtime commit.

## Manual Dispatch Example

```bash
curl -X POST "https://api.github.com/repos/${BOTS_PLATFORM_OWNER}/${BOTS_PLATFORM_REPO}/dispatches" \
  -H "Accept: application/vnd.github+json" \
  -H "Authorization: Bearer ${BOTS_PLATFORM_DISPATCH_TOKEN}" \
  -d '{
    "event_type": "bot_updated",
    "client_payload": {
      "bot_slug": "sample-echo",
      "repo": "your-org/sample-echo-bot",
      "ref": "main",
      "sha": "1111111111111111111111111111111111111111"
    }
  }'
```

## VPS Deploy

1. Install Docker Engine and Docker Compose plugin.
2. Checkout the `bots-platform` repo on the VPS.
3. Create `.env` from [.env.example](/D:/claude/боты_портфолио/.env.example).
4. Fill bot secrets and any shared infra DSNs.
5. Run:

```bash
docker compose up -d --build
```

After CI/CD is live, the GitHub Actions deploy job will switch the host to:

```bash
docker compose pull && docker compose up -d
```

## Redis, Postgres, and Workers Later

- Set `REDIS_URL` to enable the shared Redis client for modules that need caching, rate limits, or queue handoff.
- Set `POSTGRES_URL` to enable the shared Postgres client for modules that need durable state.
- Register job handlers in the shared runner and submit jobs from modules when you add background tasks.
- The one-container model stays intact: the worker loop runs in the same binary and can be toggled with `APP_MODE`.

## Example Bot Repo

See [examples/bot-repo](/D:/claude/боты_портфолио/examples/bot-repo) for a minimal declarative repo, including:

- `bot.yaml`
- copied JSON schema
- smoke tests
- `repository_dispatch` workflow

## Secrets

Platform repo / VPS:

- `BOT_<SLUG>_TOKEN`
- `BOT_<SLUG>_WEBHOOK_SECRET`
- `REDIS_URL`
- `POSTGRES_URL`
- `GHCR_USERNAME`
- `GHCR_PAT`
- `VPS_HOST`
- `VPS_USER`
- `VPS_SSH_KEY`
- `VPS_APP_DIR`
- `BOT_REPO_READ_TOKEN`

Bot repo:

- `BOTS_PLATFORM_DISPATCH_TOKEN`
- `BOTS_PLATFORM_OWNER`
- `BOTS_PLATFORM_REPO`

