// Package config отвечает за сборку, парсинг и валидацию глобальной конфигурации
// приложения из различных источников: флагов командной строки, переменных окружения и YAML-файла.
package config

import (
	"flag"
	"fmt"
	"os"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/worker"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/httpserver"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/postgres"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/slogger"
)

// Config объединяет в себе все конфигурационные подсистемы приложения:
// HTTP-сервер, базу данных, систему логирования, лимитер запросов и воркеры.
type Config struct {
	// Server содержит параметры запуска HTTP-сервера.
	Server *httpserver.Config
	// DB хранит строку подключения и параметры для PostgreSQL.
	DB *postgres.Config
	// Logger управляет конфигурацией вывода логов (консоль, файлы, уровни).
	Logger *slogger.Config
	// RLS содержит настройки подсистемы Rate Limit Store.
	RLS *ratelimitstore.Config
	// W содержит параметры конфигурации фоновых воркеров обработки.
	W *worker.Config
}

type envParser interface {
	ParseEnv() error
}

type validator interface {
	Validate() error
}

const (
	defaultConfigPath = "config/accrual.yaml"
	configPathEnvVar  = "ACCRUAL_CONFIG_PATH"
)

// Load инициализирует, считывает и валидирует полную конфигурацию приложения.
// Источники обрабатываются в следующем приоритете (от высшего к низшему):
// 1. Переменные окружения (Environment Variables)
// 2. Флаги командной строки (CLI Flags)
// 3. Значения из конфигурационного YAML-файла
// 4. Дефолтные значения подсистем.
func Load(args []string) (*Config, error) {

	fs := flag.NewFlagSet("config", flag.ContinueOnError)

	configPathFlag := fs.String("config", defaultConfigPath, "path to config file")
	serverConfig := httpserver.RegisterFlags(fs)
	dbConfig := postgres.RegisterFlags(fs)

	// Парсим флаги
	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("parse flags: %w", err)
	}

	configPath := *configPathFlag
	if v := os.Getenv(configPathEnvVar); v != "" {
		configPath = v
	}

	// Парсим env если есть, то они приоритет
	for _, p := range []envParser{serverConfig, dbConfig} {
		if err := p.ParseEnv(); err != nil {
			return nil, fmt.Errorf("parse env: %w", err)
		}
	}

	yc, err := loadYAML(configPath)
	if err != nil {
		return nil, fmt.Errorf("load yaml config: %w", err)
	}

	for _, v := range []validator{yc.Logger, yc.RLS, dbConfig, serverConfig, yc.Worker} {
		if err := v.Validate(); err != nil {
			return nil, fmt.Errorf("validate: %w", err)
		}
	}

	return &Config{
		Server: serverConfig,
		DB:     dbConfig,
		Logger: yc.Logger,
		RLS:    yc.RLS,
		W:      yc.Worker,
	}, nil

}
