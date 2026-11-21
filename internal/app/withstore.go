package app

import (
	"errors"
	"fmt"

	"github.com/O-Marsters-1997/command-center/internal/config"
	"github.com/O-Marsters-1997/command-center/internal/store"
)

func WithStore(configPath string, env config.Env, fn func(config.Config, *store.Store) error) (err error) {
	cfg, err := config.Load(configPath, env)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	st, err := store.OpenStore(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open store: %w", err)
	}
	defer func() { err = errors.Join(err, st.Close()) }()
	return fn(cfg, st)
}
