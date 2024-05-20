package zaya

import (
	"errors"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
	"time"
)

type Config struct {
	TgToken    string `koanf:"tg_token"`
	DBPath     string `koanf:"db_path"`
	RolesPath  string `koanf:"roles_path"`
	Welcome    string
	Release    bool
	DefaultCfg DefaultConfig `koanf:"default_cfg"`
	Ai         AiConfig
}

type DefaultConfig struct {
	Freq     int
	Nickname string
	Prompt   string
}

type AiConfig struct {
	Provider string
	BaseUrl  string `koanf:"base_url"`
	ApiKey   string `koanf:"api_key"`
	Model    string
	NCtx     int `koanf:"n_ctx"`
	Temp     float64
	TopK     int           `koanf:"tok_k"`
	RepPen   float64       `koanf:"rep_pen"`
	MaxTok   int           `koanf:"max_tok"`
	ExpTime  time.Duration `koanf:"exp_time"`
	ChatExp  time.Duration `koanf:"chat_exp"`
	Stop     []string
}

func LoadConfig() (Config, error) {
	var kConf = koanf.New("/")

	var cfg Config

	err := kConf.Load(file.Provider("zaya.toml"), toml.Parser())
	if err != nil {
		return cfg, err
	}

	err = kConf.Unmarshal("", &cfg)
	if err != nil {
		return cfg, err
	}

	if cfg.TgToken == "" {
		return cfg, errors.New("telegram token is required")
	}

	return cfg, nil
}
