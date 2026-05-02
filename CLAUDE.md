# CLAUDE.md

## What This Project Is

`bots-platform` is a lightweight multi-bot Telegram backend for portfolio projects.

The goal is to run many Telegram bots on a small VPS with:

- one Go binary
- one Docker image
- one backend container
- one webhook-based HTTP entrypoint
- low RAM usage
- simple deployment and maintenance

This is intentionally a modular monolith, not a microservice system.

## Why It Exists

The platform is designed for a constrained server:

- 1 GB RAM
- 1 vCPU
- low traffic
- many small portfolio bots over time

Running one container or process per bot would waste memory and complicate deploys.  
Instead, all bots share one runtime and one deployment unit.

## Core Runtime Model

- Telegram works only through webhooks, never long polling.
- Webhook route format is `/telegram/{bot_slug}/webhook`.
- Bot configs are loaded into one in-memory registry at startup.
- The runtime resolves a bot by `bot_slug`, validates the webhook secret, and dispatches the update to that bot's module.
- Shared infrastructure is initialized once and reused:
  - Telegram API client
  - generic HTTP client
  - optional Redis client
  - optional Postgres client
  - shared worker/job runner

Supported runtime modes:

- `server`
- `worker`
- `all`

For production on the small VPS, `all` is the default practical mode.

## Very Important Architectural Boundary

There are two repository types:

### 1. `bots-platform`

This repo owns all executable logic:

- Go runtime
- HTTP server
- module registry
- shared infra clients
- worker layer
- Docker and deploy logic
- registry sync and lock handling

If a new bot needs new behavior, that behavior must be implemented here as a new module.

### 2. Bot repos

Each individual bot lives in its own repository, but those repos are declarative only.

A bot repo contains things like:

- `bot.yaml`
- texts
- assets
- templates
- tests
- bot-specific metadata

A bot repo must not become its own service, container, or separate runtime.

## Non-Negotiable Rules

- Do not create one container per bot.
- Do not create one process per bot.
- Do not introduce polling.
- Do not move runtime logic into bot repos.
- Do not store Telegram tokens or webhook secrets in Git-tracked bot files.
- Do not break the single-image deployment model.

## How New Bots Are Added

1. Create a new bot repo from `examples/bot-repo/`.
2. Fill in `bot.yaml`.
3. Point `module` to an existing runtime module, if possible.
4. Register the bot in `config/bots.yml`.
5. Configure the secret env vars on the deployment target.
6. Let bot repo CI dispatch its pinned revision to `bots-platform`.
7. `bots-platform` vendors the bot snapshot into `registry/bots/<slug>/` and updates `registry/bots.lock.json`.
8. The platform builds one shared image and deploys one backend service.

## When To Add A New Module

Add a new module in `bots-platform` only when a new bot cannot be expressed by configuration/content on top of an existing module.

Current module contract:

```go
type Module interface {
    Name() string
    HandleUpdate(ctx context.Context, bot *BotRuntime, update telegram.Update) error
}
```

Current example module:

- `echo`

## Secrets Model

Secrets are referenced by env var name inside `bot.yaml`, for example:

- `telegram.token_env`
- `telegram.webhook_secret_env`

The actual secret values live only in:

- local `.env`
- VPS environment
- GitHub Secrets

## Files To Read First

If you are making changes, start here:

- `README.md`
- `config/bots.yml`
- `registry/bots.lock.json`
- `registry/schemas/bot.schema.json`
- `internal/app/app.go`
- `internal/bots/module.go`
- `internal/bots/router.go`
- `examples/bot-repo/`

## Change Guidance For Future Agents

- Prefer extending the existing runtime over adding new layers.
- Keep dependencies minimal.
- Prefer standard library HTTP and simple composition.
- Optimize for clarity, small memory footprint, and easy deploy.
- If adding Redis/Postgres usage, keep them shared and optional.
- If adding background work, use the shared worker layer instead of spawning separate services.
- Preserve the repo boundary: executable code in `bots-platform`, declarative content in bot repos.

