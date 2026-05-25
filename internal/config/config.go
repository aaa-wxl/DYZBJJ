// config 负责读取本地运行配置，并为缺省环境提供可启动的默认值。
package config

import "os"

type Config struct {
	HTTPAddr    string
	DatabaseURL string
	RedisAddr   string
	FrontendURL string
}

// Load 从环境变量加载配置，缺省值用于本地演示。
func Load() Config {
	return Config{
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
		DatabaseURL: env("DATABASE_URL", "postgres://auction:auction@localhost:5432/auction?sslmode=disable"),
		RedisAddr:   env("REDIS_ADDR", "localhost:6379"),
		FrontendURL: env("FRONTEND_URL", "http://localhost:5173"),
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
