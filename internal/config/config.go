// config 负责读取本地运行配置，并为缺省环境提供可启动的默认值。
package config

import (
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr      string
	DatabaseURL   string
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisRequired bool
	FrontendURL   string
	JWTSecret     string
	JWTTTL        time.Duration
}

// Load 从环境变量加载配置，缺省值用于本地演示。
func Load() Config {
	return Config{
		HTTPAddr:      env("HTTP_ADDR", ":8080"),
		DatabaseURL:   env("DATABASE_URL", "mysql://auction:auction@tcp(127.0.0.1:3306)/auction"),
		RedisAddr:     env("REDIS_ADDR", "127.0.0.1:6379"),
		RedisPassword: env("REDIS_PASSWORD", ""),
		RedisDB:       envInt("REDIS_DB", 0),
		RedisRequired: envBool("REDIS_REQUIRED", true),
		FrontendURL:   env("FRONTEND_URL", "http://localhost:5173"),
		JWTSecret:     env("JWT_SECRET", "local-demo-jwt-secret"),
		JWTTTL:        24 * time.Hour,
	}
}

// env 返回环境变量值；为空时使用 fallback。
func env(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func envInt(key string, fallback int) int {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func envBool(key string, fallback bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}
