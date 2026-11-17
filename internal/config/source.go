package config

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"time"

	"github.com/OXeu/configcenter/sdk/configcenter"
)

func loadSource(ctx context.Context, lookup func(string) (string, bool)) (func(string, string) string, error) {
	envFile := lookupOr(lookup, "CC_ENV_FILE", DefaultEnvFile)
	items, err := (configcenter.DotenvSource{Files: []string{envFile}}).Read(ctx, "CC_")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("load local configuration: %w", err)
	}
	fileValues := make(map[string]string, len(items))
	for _, item := range items {
		fileValues[item.Key] = item.Value
	}
	local := func(key string) (string, bool) {
		if value, ok := lookup(key); ok {
			return value, true
		}
		value, ok := fileValues[key]
		return value, ok
	}
	client, err := configcenter.New(configcenter.Config{
		BaseURL:       lookupOr(local, "CC_CONFIGCENTER_URL", ""),
		APIKey:        lookupOr(local, "CC_CONFIGCENTER_API_KEY", ""),
		Environment:   lookupOr(local, "CC_CONFIGCENTER_ENVIRONMENT", "production"),
		RevealSecrets: true,
		DotenvFiles:   []string{},
		Timeout:       5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("configure configcenter SDK: %w", err)
	}
	if err := client.LoadPrefix(ctx, "CC_"); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("load remote configuration: %w", ctx.Err())
		}
		slog.WarnContext(ctx, "remote configuration unavailable; using local configuration", "error", err)
	}
	return func(key, fallback string) string {
		if value, ok := client.Get(key); ok {
			return value
		}
		return lookupOr(local, key, fallback)
	}, nil
}
