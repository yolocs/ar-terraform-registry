package config

import (
	"context"
	"fmt"

	"github.com/sethvargo/go-envconfig"
)

type Config struct {
	Port      string `env:"PORT, default=8080"`
	ProjectID string `env:"PROJECT_ID, required"`
	Location  string `env:"LOCATION, default=us"`
}

func Load(ctx context.Context) (*Config, error) {
	var c Config
	if err := envconfig.Process(ctx, &c); err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	return &c, nil
}
