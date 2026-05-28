package config

import "testing"

func TestLoadRedisConfigDefaults(t *testing.T) {
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "")
	t.Setenv("REDIS_REQUIRED", "")

	cfg := Load()

	if cfg.RedisAddr != "127.0.0.1:6379" {
		t.Fatalf("RedisAddr = %q", cfg.RedisAddr)
	}
	if cfg.RedisPassword != "" {
		t.Fatalf("RedisPassword = %q", cfg.RedisPassword)
	}
	if cfg.RedisDB != 0 {
		t.Fatalf("RedisDB = %d", cfg.RedisDB)
	}
	if !cfg.RedisRequired {
		t.Fatal("RedisRequired should default to true")
	}
}

func TestLoadRedisConfigFromEnv(t *testing.T) {
	t.Setenv("REDIS_ADDR", "redis.internal:6380")
	t.Setenv("REDIS_PASSWORD", "secret")
	t.Setenv("REDIS_DB", "2")
	t.Setenv("REDIS_REQUIRED", "false")

	cfg := Load()

	if cfg.RedisAddr != "redis.internal:6380" {
		t.Fatalf("RedisAddr = %q", cfg.RedisAddr)
	}
	if cfg.RedisPassword != "secret" {
		t.Fatalf("RedisPassword = %q", cfg.RedisPassword)
	}
	if cfg.RedisDB != 2 {
		t.Fatalf("RedisDB = %d", cfg.RedisDB)
	}
	if cfg.RedisRequired {
		t.Fatal("RedisRequired should be false")
	}
}
