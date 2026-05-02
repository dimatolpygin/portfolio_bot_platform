package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/example/bots-platform/internal/bots"
	"github.com/example/bots-platform/internal/bots/modules/echo"
	"github.com/example/bots-platform/internal/registry"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	var err error

	switch os.Args[1] {
	case "validate":
		err = runValidate(os.Args[2:])
	case "sync-bot":
		err = runSyncBot(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "botsctl error: %v\n", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage:")
	fmt.Fprintln(os.Stderr, "  botsctl validate [-config config/bots.yml] [-lock registry/bots.lock.json] [-bots-dir registry/bots]")
	fmt.Fprintln(os.Stderr, "  botsctl sync-bot -slug <slug> -repo <owner/repo> -ref <ref> -sha <sha> -source <dir> [-snapshot-path .] [-lock registry/bots.lock.json] [-registry-dir registry/bots]")
}

func runValidate(args []string) error {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	configPath := fs.String("config", "config/bots.yml", "path to bot catalog")
	lockPath := fs.String("lock", "registry/bots.lock.json", "path to bot lock file")
	botsDir := fs.String("bots-dir", "registry/bots", "path to vendored bot manifests")

	if err := fs.Parse(args); err != nil {
		return err
	}

	modules, err := bots.NewModuleRegistry(echo.New())
	if err != nil {
		return err
	}

	store, err := registry.Load(registry.LoadOptions{
		CatalogPath:    *configPath,
		LockPath:       *lockPath,
		BotsDir:        *botsDir,
		AllowedModules: modules.Names(),
		Logger:         discardLogger(),
	})
	if err != nil {
		return err
	}

	fmt.Printf("validated %d bots\n", store.Count())
	return nil
}

func runSyncBot(args []string) error {
	fs := flag.NewFlagSet("sync-bot", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	slug := fs.String("slug", "", "bot slug")
	repo := fs.String("repo", "", "bot repository in owner/repo format")
	ref := fs.String("ref", "main", "git ref")
	sha := fs.String("sha", "", "pinned commit SHA")
	source := fs.String("source", "", "source directory that contains bot.yaml")
	snapshotPath := fs.String("snapshot-path", ".", "relative path inside the bot repo that was synced")
	lockPath := fs.String("lock", "registry/bots.lock.json", "path to bot lock file")
	registryDir := fs.String("registry-dir", "registry/bots", "path to vendored bot snapshots")

	if err := fs.Parse(args); err != nil {
		return err
	}

	switch {
	case *slug == "":
		return errors.New("slug is required")
	case *repo == "":
		return errors.New("repo is required")
	case *sha == "":
		return errors.New("sha is required")
	case *source == "":
		return errors.New("source is required")
	}

	manifestPath := filepath.Join(*source, "bot.yaml")
	manifest, err := registry.LoadManifest(manifestPath)
	if err != nil {
		return fmt.Errorf("load manifest: %w", err)
	}

	if manifest.Slug != *slug {
		return fmt.Errorf("manifest slug %q does not match sync slug %q", manifest.Slug, *slug)
	}

	targetDir := filepath.Join(*registryDir, *slug)
	if err := os.RemoveAll(targetDir); err != nil {
		return fmt.Errorf("reset target dir: %w", err)
	}
	if err := copyTree(*source, targetDir); err != nil {
		return fmt.Errorf("copy bot snapshot: %w", err)
	}

	lockFile, err := registry.LoadLock(*lockPath)
	if err != nil {
		return fmt.Errorf("load lock: %w", err)
	}

	lockFile.Upsert(registry.LockEntry{
		Slug:         *slug,
		Repo:         *repo,
		Ref:          *ref,
		SHA:          *sha,
		SnapshotPath: *snapshotPath,
		SyncedAt:     time.Now().UTC(),
	})

	if err := registry.WriteLock(*lockPath, lockFile); err != nil {
		return fmt.Errorf("write lock: %w", err)
	}

	fmt.Printf("synced %s from %s at %s\n", *slug, *repo, *sha)
	return nil
}

func copyTree(sourceDir, targetDir string) error {
	return filepath.WalkDir(sourceDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(sourceDir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(targetDir, 0o755)
		}
		if rel == ".git" {
			return filepath.SkipDir
		}

		targetPath := filepath.Join(targetDir, rel)
		if d.IsDir() {
			return os.MkdirAll(targetPath, 0o755)
		}

		return copyFile(path, targetPath)
	})
}

func copyFile(sourcePath, targetPath string) error {
	source, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer source.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}

	target, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer target.Close()

	if _, err := io.Copy(target, source); err != nil {
		return err
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return err
	}

	return os.Chmod(targetPath, info.Mode())
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
