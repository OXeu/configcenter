package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/OXeu/configcenter/sdk/configcenter"
)

// Loader separates local database bootstrap settings from stored runtime settings.
type Loader struct {
	DatabaseDriver string
	DatabaseDSN    string
	Bootstrap      Bootstrap
	local          func(string, string) string
}

// Prepare reads local bootstrap settings without opening a database or HTTP connection.
func Prepare(ctx context.Context, lookup func(string) (string, bool)) (*Loader, error) {
	envFile := lookupOr(lookup, "CC_ENV_FILE", DefaultEnvFile)
	file, err := configcenter.LoadSnapshot(ctx, "CC_", configcenter.DotenvSource{Files: []string{envFile}})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("load local configuration: %w", err)
	}
	local := func(key, fallback string) string {
		if value, ok := lookup(key); ok {
			if value != "" {
				return value
			}
			return fallback
		}
		if value, ok := file.Get(key); ok && value != "" {
			return value
		}
		return fallback
	}
	driver := local("CC_DB_DRIVER", "sqlite")
	dsn, err := normalizeDatabase(driver, local("CC_DB", "file:configcenter.db"))
	if err != nil {
		return nil, err
	}
	return &Loader{
		DatabaseDriver: driver,
		DatabaseDSN:    dsn,
		Bootstrap: Bootstrap{
			AdminEmail: local("CC_BOOTSTRAP_ADMIN_EMAIL", "admin@localhost"),
			AdminName:  local("CC_BOOTSTRAP_ADMIN_NAME", "admin"),
		},
		local: local,
	}, nil
}

// Load resolves stored runtime settings before local fallbacks, keeping bootstrap settings local.
func (l *Loader) Load(ctx context.Context, source configcenter.DataSource) (Config, error) {
	snapshot, err := configcenter.LoadSnapshot(ctx, "CC_", source)
	if err != nil {
		return Config{}, fmt.Errorf("load stored configuration: %w", err)
	}
	return fromSnapshot(func(key, fallback string) string {
		switch key {
		case "CC_DB_DRIVER":
			return l.DatabaseDriver
		case "CC_DB":
			return l.DatabaseDSN
		case "CC_BOOTSTRAP_ADMIN_EMAIL":
			return l.Bootstrap.AdminEmail
		case "CC_BOOTSTRAP_ADMIN_NAME":
			return l.Bootstrap.AdminName
		default:
			return snapshot.GetDefault(key, l.local(key, fallback))
		}
	})
}
