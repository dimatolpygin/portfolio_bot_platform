package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}$`)

func Load(options LoadOptions) (*Store, error) {
	if options.CatalogPath == "" {
		return nil, errors.New("catalog path is required")
	}
	if options.LockPath == "" {
		return nil, errors.New("lock path is required")
	}
	if options.BotsDir == "" {
		return nil, errors.New("bots directory is required")
	}

	catalog, err := LoadCatalog(options.CatalogPath)
	if err != nil {
		return nil, err
	}

	lockFile, err := LoadLock(options.LockPath)
	if err != nil {
		return nil, err
	}

	lockBySlug := make(map[string]LockEntry, len(lockFile.Bots))
	for _, entry := range lockFile.Bots {
		if entry.Slug == "" {
			return nil, errors.New("lock entry slug is required")
		}
		if _, exists := lockBySlug[entry.Slug]; exists {
			return nil, fmt.Errorf("duplicate lock entry for slug %q", entry.Slug)
		}
		lockBySlug[entry.Slug] = entry
	}

	bots := make(map[string]*BotConfig, len(catalog.Bots))
	for _, entry := range catalog.Bots {
		if err := validateCatalogEntry(entry); err != nil {
			return nil, err
		}
		if _, exists := bots[entry.Slug]; exists {
			return nil, fmt.Errorf("duplicate catalog entry for slug %q", entry.Slug)
		}

		manifestPath := filepath.Join(options.BotsDir, entry.Slug, "bot.yaml")
		manifest, err := LoadManifest(manifestPath)
		if err != nil {
			return nil, fmt.Errorf("load manifest for %q: %w", entry.Slug, err)
		}

		if manifest.Slug != entry.Slug {
			return nil, fmt.Errorf("manifest slug %q does not match catalog slug %q", manifest.Slug, entry.Slug)
		}

		if err := manifest.Validate(options.AllowedModules); err != nil {
			return nil, fmt.Errorf("validate manifest for %q: %w", entry.Slug, err)
		}

		secrets, err := resolveSecrets(manifest)
		if err != nil {
			return nil, fmt.Errorf("resolve secrets for %q: %w", entry.Slug, err)
		}

		var lockEntry *LockEntry
		if entryValue, ok := lockBySlug[entry.Slug]; ok {
			if entryValue.Repo != entry.Repo {
				return nil, fmt.Errorf("lock repo %q does not match catalog repo %q for slug %q", entryValue.Repo, entry.Repo, entry.Slug)
			}
			copyValue := entryValue
			lockEntry = &copyValue
		}

		bots[entry.Slug] = &BotConfig{
			Catalog:     entry,
			Lock:        lockEntry,
			Manifest:    manifest,
			SnapshotDir: filepath.Join(options.BotsDir, entry.Slug),
			Secrets:     secrets,
		}
	}

	for slug := range lockBySlug {
		if _, exists := bots[slug]; !exists {
			return nil, fmt.Errorf("lock entry exists for unknown slug %q", slug)
		}
	}

	return &Store{bots: bots}, nil
}

func LoadCatalog(path string) (Catalog, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Catalog{}, fmt.Errorf("read catalog %q: %w", path, err)
	}

	var catalog Catalog
	if err := yaml.Unmarshal(raw, &catalog); err != nil {
		return Catalog{}, fmt.Errorf("decode catalog %q: %w", path, err)
	}

	return catalog, nil
}

func LoadLock(path string) (*LockFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &LockFile{Version: 1, Bots: []LockEntry{}}, nil
		}
		return nil, fmt.Errorf("read lock %q: %w", path, err)
	}

	var lock LockFile
	if err := json.Unmarshal(raw, &lock); err != nil {
		return nil, fmt.Errorf("decode lock %q: %w", path, err)
	}
	if lock.Version == 0 {
		lock.Version = 1
	}
	if lock.Bots == nil {
		lock.Bots = []LockEntry{}
	}

	return &lock, nil
}

func WriteLock(path string, lock *LockFile) error {
	if lock == nil {
		return errors.New("lock is required")
	}

	sort.Slice(lock.Bots, func(i, j int) bool {
		return lock.Bots[i].Slug < lock.Bots[j].Slug
	})

	payload, err := json.MarshalIndent(lock, "", "  ")
	if err != nil {
		return fmt.Errorf("encode lock: %w", err)
	}
	payload = append(payload, '\n')

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write lock %q: %w", path, err)
	}
	return nil
}

func LoadManifest(path string) (Manifest, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, fmt.Errorf("read manifest %q: %w", path, err)
	}

	var manifest Manifest
	if err := yaml.Unmarshal(raw, &manifest); err != nil {
		return Manifest{}, fmt.Errorf("decode manifest %q: %w", path, err)
	}

	return manifest, nil
}

func (m Manifest) Validate(allowedModules map[string]struct{}) error {
	switch {
	case strings.TrimSpace(m.Slug) == "":
		return errors.New("slug is required")
	case !slugPattern.MatchString(m.Slug):
		return fmt.Errorf("slug %q must match %s", m.Slug, slugPattern.String())
	case strings.TrimSpace(m.Name) == "":
		return errors.New("name is required")
	case strings.TrimSpace(m.Description) == "":
		return errors.New("description is required")
	case m.Enabled == nil:
		return errors.New("enabled is required")
	case strings.TrimSpace(m.Module) == "":
		return errors.New("module is required")
	case strings.TrimSpace(m.WelcomeMessage) == "":
		return errors.New("welcome_message is required")
	case strings.TrimSpace(m.Telegram.TokenEnv) == "":
		return errors.New("telegram.token_env is required")
	case strings.TrimSpace(m.Telegram.WebhookSecretEnv) == "":
		return errors.New("telegram.webhook_secret_env is required")
	}

	if len(allowedModules) > 0 {
		if _, ok := allowedModules[m.Module]; !ok {
			return fmt.Errorf("module %q is not registered in runtime", m.Module)
		}
	}

	jobNames := make(map[string]struct{}, len(m.Jobs))
	for _, job := range m.Jobs {
		if strings.TrimSpace(job.Name) == "" {
			return errors.New("job name is required")
		}
		if _, exists := jobNames[job.Name]; exists {
			return fmt.Errorf("duplicate job name %q", job.Name)
		}
		jobNames[job.Name] = struct{}{}
	}

	envNames := make(map[string]struct{}, len(m.EnvSchema))
	for _, item := range m.EnvSchema {
		if strings.TrimSpace(item.Name) == "" {
			return errors.New("env_schema.name is required")
		}
		if _, exists := envNames[item.Name]; exists {
			return fmt.Errorf("duplicate env_schema name %q", item.Name)
		}
		envNames[item.Name] = struct{}{}
	}

	return nil
}

func validateCatalogEntry(entry CatalogEntry) error {
	switch {
	case strings.TrimSpace(entry.Slug) == "":
		return errors.New("catalog slug is required")
	case !slugPattern.MatchString(entry.Slug):
		return fmt.Errorf("catalog slug %q must match %s", entry.Slug, slugPattern.String())
	case strings.TrimSpace(entry.Repo) == "":
		return fmt.Errorf("catalog repo is required for slug %q", entry.Slug)
	case strings.TrimSpace(entry.DefaultRef) == "":
		return fmt.Errorf("catalog default_ref is required for slug %q", entry.Slug)
	}

	return nil
}

func resolveSecrets(manifest Manifest) (ResolvedSecrets, error) {
	token := strings.TrimSpace(os.Getenv(manifest.Telegram.TokenEnv))
	secret := strings.TrimSpace(os.Getenv(manifest.Telegram.WebhookSecretEnv))

	if manifest.IsEnabled() {
		if token == "" {
			return ResolvedSecrets{}, fmt.Errorf("env %s is required for enabled bot", manifest.Telegram.TokenEnv)
		}
		if secret == "" {
			return ResolvedSecrets{}, fmt.Errorf("env %s is required for enabled bot", manifest.Telegram.WebhookSecretEnv)
		}
	}

	return ResolvedSecrets{
		Token:         token,
		WebhookSecret: secret,
	}, nil
}
