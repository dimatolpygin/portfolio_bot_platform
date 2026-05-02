package registry

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadResolvesEnabledBotSecrets(t *testing.T) {
	t.Setenv("BOT_SAMPLE_TOKEN", "123:token")
	t.Setenv("BOT_SAMPLE_SECRET", "secret-token")

	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "config", "bots.yml"), `bots:
  - slug: sample-echo
    repo: your-org/sample-echo-bot
    default_ref: main
    snapshot_path: .
`)
	writeTestFile(t, filepath.Join(root, "registry", "bots.lock.json"), `{
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
`)
	writeTestFile(t, filepath.Join(root, "registry", "bots", "sample-echo", "bot.yaml"), `slug: sample-echo
name: Sample Echo Bot
description: Sample
enabled: true
module: echo
welcome_message: Hello
telegram:
  token_env: BOT_SAMPLE_TOKEN
  webhook_secret_env: BOT_SAMPLE_SECRET
`)

	store, err := Load(LoadOptions{
		CatalogPath: filepath.Join(root, "config", "bots.yml"),
		LockPath:    filepath.Join(root, "registry", "bots.lock.json"),
		BotsDir:     filepath.Join(root, "registry", "bots"),
		AllowedModules: map[string]struct{}{
			"echo": {},
		},
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	bot, ok := store.Bot("sample-echo")
	if !ok {
		t.Fatalf("expected bot to be loaded")
	}
	if bot.Secrets.Token != "123:token" {
		t.Fatalf("unexpected token: %q", bot.Secrets.Token)
	}
	if bot.Secrets.WebhookSecret != "secret-token" {
		t.Fatalf("unexpected webhook secret: %q", bot.Secrets.WebhookSecret)
	}
	if bot.Lock == nil || bot.Lock.SHA == "" {
		t.Fatalf("expected lock entry to be attached")
	}
}

func TestLoadFailsWhenEnabledBotSecretMissing(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "config", "bots.yml"), `bots:
  - slug: sample-echo
    repo: your-org/sample-echo-bot
    default_ref: main
    snapshot_path: .
`)
	writeTestFile(t, filepath.Join(root, "registry", "bots.lock.json"), `{"version":1,"bots":[]}`)
	writeTestFile(t, filepath.Join(root, "registry", "bots", "sample-echo", "bot.yaml"), `slug: sample-echo
name: Sample Echo Bot
description: Sample
enabled: true
module: echo
welcome_message: Hello
telegram:
  token_env: BOT_SAMPLE_TOKEN
  webhook_secret_env: BOT_SAMPLE_SECRET
`)

	_, err := Load(LoadOptions{
		CatalogPath: filepath.Join(root, "config", "bots.yml"),
		LockPath:    filepath.Join(root, "registry", "bots.lock.json"),
		BotsDir:     filepath.Join(root, "registry", "bots"),
		AllowedModules: map[string]struct{}{
			"echo": {},
		},
	})
	if err == nil {
		t.Fatalf("expected Load() to fail when env vars are missing")
	}
}

func TestWriteLockSortsEntries(t *testing.T) {
	root := t.TempDir()
	lockPath := filepath.Join(root, "registry", "bots.lock.json")

	lock := &LockFile{
		Version: 1,
		Bots: []LockEntry{
			{Slug: "zeta"},
			{Slug: "alpha"},
		},
	}

	if err := WriteLock(lockPath, lock); err != nil {
		t.Fatalf("WriteLock() error = %v", err)
	}

	raw, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}

	expected := "{\n  \"version\": 1,\n  \"bots\": [\n    {\n      \"slug\": \"alpha\",\n      \"repo\": \"\",\n      \"ref\": \"\",\n      \"sha\": \"\",\n      \"snapshot_path\": \"\",\n      \"synced_at\": \"0001-01-01T00:00:00Z\"\n    },\n    {\n      \"slug\": \"zeta\",\n      \"repo\": \"\",\n      \"ref\": \"\",\n      \"sha\": \"\",\n      \"snapshot_path\": \"\",\n      \"synced_at\": \"0001-01-01T00:00:00Z\"\n    }\n  ]\n}\n"
	if string(raw) != expected {
		t.Fatalf("unexpected lock output:\n%s", string(raw))
	}
}

func writeTestFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}

