package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestLoad_Defaults(t *testing.T) {
	t.Setenv("SERVER_ADDR", "")
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("DATABASE_URL", "")

	cfg := Load()

	assert.Equal(t, ":8080", cfg.ServerAddr)
	assert.Equal(t, "localhost:6379", cfg.RedisAddr)
	assert.Empty(t, cfg.DatabaseURL)
}

func TestLoad_CustomEnvVars(t *testing.T) {
	t.Setenv("SERVER_ADDR", ":9090")
	t.Setenv("REDIS_ADDR", "redis:6380")
	t.Setenv("DATABASE_URL", "postgres://user:pass@db:5432/quiz")

	cfg := Load()

	assert.Equal(t, ":9090", cfg.ServerAddr)
	assert.Equal(t, "redis:6380", cfg.RedisAddr)
	assert.Equal(t, "postgres://user:pass@db:5432/quiz", cfg.DatabaseURL)
}
