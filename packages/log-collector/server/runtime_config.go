package server

import (
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/config"
	"github.com/X-code-interpreter/sandbox-backend/packages/shared/consts"
)

type LogCollectorConfig struct {
	Port     int    `toml:"port"`
	DataRoot string `toml:"_"`
}

func ParseLogCollectorConfig(configFile string) (*LogCollectorConfig, error) {
	var (
		err          error
		globalConfig struct {
			config.CommonConfig
			LogCollectorCfg toml.Primitive `toml:"log_collector"`
		}
		cfg LogCollectorConfig
	)
	if configFile == "" {
		configFile, err = config.GetConfigFilePath()
		if err != nil {
			return nil, err
		}
	}
	meta, err := toml.DecodeFile(configFile, &globalConfig)
	if err != nil {
		return nil, err
	}
	if err = meta.PrimitiveDecode(globalConfig.LogCollectorCfg, &cfg); err != nil {
		return nil, err
	}
	cfg.DataRoot = globalConfig.CommonConfig.DataRoot
	if cfg.Port == 0 {
		cfg.Port = consts.DefaultLogCollectorPort
	}
	return &cfg, nil
}

func (cfg *LogCollectorConfig) LogDir() string {
	return filepath.Join(cfg.DataRoot, consts.EnvdLogDirName)
}
