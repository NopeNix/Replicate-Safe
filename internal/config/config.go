package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	APIToken      string
	OutputDir     string
	StateFile     string
	PollInterval  time.Duration
	HTTPTimeout   time.Duration
	WriteMetadata bool
	LogLevel      string
}

func Load() (*Config, error) {
	token := os.Getenv("REPLICATE_API_TOKEN")
	if token == "" {
		return nil, errors.New("REPLICATE_API_TOKEN is required")
	}

	outputDir := getenv("OUTPUT_DIR", "/data")
	stateFile := getenv("STATE_FILE", "/data/.state.json")

	poll, err := durationEnv("POLL_INTERVAL", 15*time.Minute)
	if err != nil {
		return nil, err
	}
	if poll <= 0 {
		return nil, errors.New("POLL_INTERVAL must be > 0")
	}

	httpTimeout, err := durationEnv("HTTP_TIMEOUT", 60*time.Second)
	if err != nil {
		return nil, err
	}

	writeMeta, err := boolEnv("WRITE_METADATA", true)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		APIToken:      token,
		OutputDir:     outputDir,
		StateFile:     stateFile,
		PollInterval:  poll,
		HTTPTimeout:   httpTimeout,
		WriteMetadata: writeMeta,
		LogLevel:      getenv("LOG_LEVEL", "info"),
	}
	return cfg, nil
}

func getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func durationEnv(key string, def time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s: must be an integer number of seconds: %w", key, err)
	}
	return time.Duration(n) * time.Second, nil
}

func boolEnv(key string, def bool) (bool, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return def, nil
	}
	switch raw {
	case "1", "true", "TRUE", "True", "yes", "YES":
		return true, nil
	case "0", "false", "FALSE", "False", "no", "NO":
		return false, nil
	}
	return false, fmt.Errorf("%s: must be one of true/false/1/0/yes/no", key)
}
