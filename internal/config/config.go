package config

import (
	"errors"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// reloadDebounce coalesces the burst of fsnotify events a single file save
// can produce (e.g. `cat > file` truncates then writes, firing two events;
// reacting to the first would read a momentarily-empty file). Waiting this
// long after the last event before re-reading means only the settled
// on-disk content ever reaches onChange.
const reloadDebounce = 150 * time.Millisecond

type Config struct {
	DatabaseURL string
	Port        string
}

// Watch loads Config from v — flags, env vars, an optional YAML file, and
// defaults, in viper's own precedence order — and returns the initial
// value. Pass "" for configPath to skip an explicit file and instead
// discover pavedway.* in the working directory.
//
// If onChange is non-nil, Watch also starts watching the config file and
// invokes onChange with the newly loaded Config on every change. A reload
// that fails to parse (e.g. mid-write) is dropped — the previous config
// keeps serving rather than crashing the process on a transient bad read.
func Watch(v *viper.Viper, configPath string, onChange func(Config)) (Config, error) {
	v.AutomaticEnv()
	v.SetDefault("port", "8080")

	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("pavedway")
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if configPath != "" || !errors.As(err, &notFound) {
			return Config{}, err
		}
		// no default pavedway.* file in cwd — flags/env/defaults still apply
	}

	cfg, err := build(v)
	if err != nil {
		return Config{}, err
	}

	if onChange != nil {
		var mu sync.Mutex
		var timer *time.Timer

		v.OnConfigChange(func(_ fsnotify.Event) {
			mu.Lock()
			defer mu.Unlock()

			if timer != nil {
				timer.Stop()
			}
			timer = time.AfterFunc(reloadDebounce, func() {
				if cfg, err := build(v); err == nil {
					onChange(cfg)
				}
			})
		})
		v.WatchConfig()
	}

	return cfg, nil
}

func build(v *viper.Viper) (Config, error) {
	dbURL := v.GetString("database_url")
	if dbURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}

	return Config{
		DatabaseURL: dbURL,
		Port:        v.GetString("port"),
	}, nil
}
