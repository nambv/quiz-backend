package config

import (
	"cmp"
	"os"
)

// Config holds application configuration loaded from environment variables.
type Config struct {
	ServerAddr  string
	RedisAddr   string
	DatabaseURL string
}

// Load returns a Config populated from environment variables with sensible defaults.
func Load() Config {
	return Config{
		ServerAddr:  cmp.Or(os.Getenv("SERVER_ADDR"), ":8080"),
		RedisAddr:   cmp.Or(os.Getenv("REDIS_ADDR"), "localhost:6379"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
	}
}
