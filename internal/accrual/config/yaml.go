package config

import (
	"errors"
	"fmt"
	"os"

	_ "embed"

	"github.com/Sayfargo/gophermart-loyal-service/internal/accrual/worker"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/ratelimitstore"
	"github.com/Sayfargo/gophermart-loyal-service/pkg/slogger"
	"github.com/stretchr/testify/assert/yaml"
)

type yamlConfig struct {
	Logger *slogger.Config        `yaml:"logger"`
	RLS    *ratelimitstore.Config `yaml:"rate_limits"`
	Worker *worker.Config         `yaml:"worker"`
}

//go:embed accrual.yaml
var defaultConfigYAML []byte

func loadYAML(path string) (*yamlConfig, error) {
	var data []byte

	if path != "" {
		file, err := os.ReadFile(path)
		if err == nil {
			data = file
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read config file: %w", err)
		}
	}

	if len(data) == 0 {
		data = defaultConfigYAML
	}

	var cfg yamlConfig

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config file: %w", err)
	}

	return &cfg, nil
}
