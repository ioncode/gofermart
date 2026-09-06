package config

import (
	"fmt"
	"os"
	"time"

	"github.com/ioncode/confy"
)

type Config struct {
	RunAddress  string        `flag:"a" env:"RUN_ADDRESS" default:"localhost:8080" desc:"address and port to run server"`
	DatabaseURI string        `flag:"d" env:"DATABASE_URI" required:"true"         desc:"database connection uri"`
	JWTSecret   string        `env:"JWT_SECRET"           default:"super-secret-key-change-me-in-production" desc:"jwt secret key"`
	TokenTTL    time.Duration `env:"TOKEN_TTL"           default:"24h"                                        desc:"token time to live"`
}

func Load() (*Config, error) {
	var cfg Config

	// Инициализируем confy компактным способом через аргументы
	// Первый параметр — префикс env-переменных. Оставляем "", так как по ТЗ префикса нет.
	// Второй параметр — слайс аргументов командной строки.
	loader := confy.New("", os.Args[1:])

	// Загружаем и валидируем (автоматически отработают default и required)
	if err := loader.Load(&cfg); err != nil {
		return nil, fmt.Errorf("failed to load configuration via confy loader: %w", err)
	}

	return &cfg, nil
}
