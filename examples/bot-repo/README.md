# Example Bot Repo

This repo is intentionally declarative. It does not build or deploy a standalone bot service.

## What lives here

- `bot.yaml`: manifest consumed by `bots-platform`
- `texts/` and `assets/`: bot-specific content
- `tests/`: schema validation and smoke checks
- `.github/workflows/ci.yml`: validates the manifest and dispatches updates to `bots-platform`

## Required GitHub Secrets

- `BOTS_PLATFORM_DISPATCH_TOKEN`: PAT or fine-grained token allowed to call `repository_dispatch` on `bots-platform`
- `BOTS_PLATFORM_OWNER`: GitHub owner of the central runtime repo
- `BOTS_PLATFORM_REPO`: repository name of the central runtime repo

## Local Validation

```bash
python tests/validate_manifest.py bot.yaml schemas/bot.schema.json
chmod +x tests/smoke.sh
tests/smoke.sh
```

## Runtime Contract

- `module` must reference a module implemented in `bots-platform`
- `telegram.token_env` and `telegram.webhook_secret_env` must name environment variables configured on the shared platform host
- secrets never belong in this repo

