package registry

import "time"

type Catalog struct {
	Bots []CatalogEntry `yaml:"bots"`
}

type CatalogEntry struct {
	Slug         string `yaml:"slug"`
	Repo         string `yaml:"repo"`
	DefaultRef   string `yaml:"default_ref"`
	SnapshotPath string `yaml:"snapshot_path"`
	Notes        string `yaml:"notes,omitempty"`
}

type LockFile struct {
	Version int         `json:"version"`
	Bots    []LockEntry `json:"bots"`
}

type LockEntry struct {
	Slug         string    `json:"slug"`
	Repo         string    `json:"repo"`
	Ref          string    `json:"ref"`
	SHA          string    `json:"sha"`
	SnapshotPath string    `json:"snapshot_path"`
	SyncedAt     time.Time `json:"synced_at"`
}

type TelegramConfig struct {
	TokenEnv         string `yaml:"token_env" json:"token_env"`
	WebhookSecretEnv string `yaml:"webhook_secret_env" json:"webhook_secret_env"`
}

type JobSpec struct {
	Name     string `yaml:"name" json:"name"`
	Enabled  bool   `yaml:"enabled" json:"enabled"`
	Schedule string `yaml:"schedule,omitempty" json:"schedule,omitempty"`
}

type EnvVarSpec struct {
	Name        string `yaml:"name" json:"name"`
	Required    bool   `yaml:"required" json:"required"`
	Secret      bool   `yaml:"secret" json:"secret"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

type Manifest struct {
	Slug           string                 `yaml:"slug" json:"slug"`
	Name           string                 `yaml:"name" json:"name"`
	Description    string                 `yaml:"description" json:"description"`
	Enabled        *bool                  `yaml:"enabled" json:"enabled"`
	Module         string                 `yaml:"module" json:"module"`
	WelcomeMessage string                 `yaml:"welcome_message" json:"welcome_message"`
	Telegram       TelegramConfig         `yaml:"telegram" json:"telegram"`
	Features       map[string]bool        `yaml:"features,omitempty" json:"features,omitempty"`
	Jobs           []JobSpec              `yaml:"jobs,omitempty" json:"jobs,omitempty"`
	Integrations   map[string]any         `yaml:"integrations,omitempty" json:"integrations,omitempty"`
	EnvSchema      []EnvVarSpec           `yaml:"env_schema,omitempty" json:"env_schema,omitempty"`
}

type ResolvedSecrets struct {
	Token         string
	WebhookSecret string
}

type BotConfig struct {
	Catalog     CatalogEntry
	Lock        *LockEntry
	Manifest    Manifest
	SnapshotDir string
	Secrets     ResolvedSecrets
}

type LoadOptions struct {
	CatalogPath    string
	LockPath       string
	BotsDir        string
	AllowedModules map[string]struct{}
	Logger         Logger
}

type Store struct {
	bots map[string]*BotConfig
}

type Logger interface {
	Debug(msg string, args ...any)
	Info(msg string, args ...any)
	Warn(msg string, args ...any)
}

func (m Manifest) IsEnabled() bool {
	return m.Enabled != nil && *m.Enabled
}

func (s *Store) Bot(slug string) (*BotConfig, bool) {
	if s == nil {
		return nil, false
	}
	bot, ok := s.bots[slug]
	return bot, ok
}

func (s *Store) Count() int {
	if s == nil {
		return 0
	}
	return len(s.bots)
}

func (s *Store) List() []*BotConfig {
	if s == nil {
		return nil
	}

	list := make([]*BotConfig, 0, len(s.bots))
	for _, bot := range s.bots {
		list = append(list, bot)
	}
	return list
}

func (lf *LockFile) Upsert(entry LockEntry) {
	for index, current := range lf.Bots {
		if current.Slug == entry.Slug {
			lf.Bots[index] = entry
			return
		}
	}
	lf.Bots = append(lf.Bots, entry)
}

