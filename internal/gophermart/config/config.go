// Package config отвечает за сборку, парсинг и валидацию глобальной конфигурации
// основного сервиса gophermart из флагов командной строки, переменных окружения и YAML-файла.
package config

import (
	"flag"
	"fmt"
	"os"

	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/accrualclient"
	"github.com/Sayfargo/gophermart-loyal-service/internal/gophermart/worker"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/httpserver"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/postgres"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/slogger"
)

// Config объединяет в себе конфигурационные параметры всех подсистем gophermart:
// HTTP-сервера, клиента системы начислений, базы данных, логгера, лимитера запросов и воркеров.
type Config struct {
	// Server содержит параметры запуска HTTP-сервера gophermart.
	Server *httpserver.Config
	// Accrual хранит настройки подключения к внешнему микросервису расчета баллов.
	Accrual *accrualclient.Config
	// DB содержит строку подключения и параметры пула для PostgreSQL.
	DB *postgres.Config
	// Logger управляет конфигурацией вывода структурированных логов.
	Logger *slogger.Config
	// RLS содержит настройки InMemory-хранилища лимитов частоты запросов.
	RLS *ratelimitstore.Config
	// W определяет параметры производительности и таймингов фоновых воркеров.
	W *worker.Config
}

type envParser interface {
	ParseEnv() error
}

type validator interface {
	Validate() error
}

const (
	configPathEnvVar = "GOPHERMART_CONFIG_PATH"
)

// Load выполняет пошаговую инициализацию, слияние и валидацию полной конфигурации сервиса.
// Приоритет применения источников (от высшего к низшему):
// 1. Переменные окружения (Environment Variables)
// 2. Флаги командной строки (CLI Flags)
// 3. Значения из файла конфигурации YAML (если путь передан)
// 4. Дефолтные значения внутренних структур.
func Load(args []string) (*Config, error) {
	fs := flag.NewFlagSet("config", flag.ContinueOnError)

	configPathFlag := fs.String("config", "", "path to config file")
	serverConfig := httpserver.RegisterFlags(fs)
	dbConfig := postgres.RegisterFlags(fs)
	accrualConfig := accrualclient.RegisterFlags(fs)

	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("parse flags: %w", err)
	}

	configPath := *configPathFlag
	if v := os.Getenv(configPathEnvVar); v != "" {
		configPath = v
	}

	for _, p := range []envParser{serverConfig, dbConfig, accrualConfig} {
		if err := p.ParseEnv(); err != nil {
			return nil, fmt.Errorf("parse env: %w", err)
		}
	}

	yc, err := loadYAML(configPath)
	if err != nil {
		return nil, fmt.Errorf("load yaml config: %w", err)
	}

	for _, v := range []validator{yc.Logger, yc.RLS, yc.Worker, dbConfig, serverConfig, accrualConfig} {
		if err := v.Validate(); err != nil {
			return nil, fmt.Errorf("validate: %w", err)
		}
	}

	return &Config{
		Server:  serverConfig,
		Accrual: accrualConfig,
		DB:      dbConfig,
		Logger:  yc.Logger,
		RLS:     yc.RLS,
		W:       yc.Worker,
	}, nil

}
