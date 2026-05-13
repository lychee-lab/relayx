package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

const (
	appDirName        = ".relayx"
	configFileName    = "config.toml"
	defaultListenAddr = "127.0.0.1:8787"
	defaultCodexBin   = "codex"
	defaultCodexMode  = "disabled"
	defaultFeishuURL  = "https://open.feishu.cn/open-apis"
	defaultFeishuMode = "long_connection"
)

type Config struct {
	ConfigPath        string
	ListenAddr        string
	CodexBin          string
	CodexMode         string
	RuntimeDir        string
	DBPath            string
	AuditPath         string
	AllowedRepos      []string
	AuthorizedUsers   []string
	FeishuAppID       string
	FeishuAppSecret   string
	FeishuBaseURL     string
	FeishuVerifyToken string
	FeishuReceiveMode string
	FeishuConfigured  bool
}

type Summary struct {
	ConfigPath        string   `json:"config_path"`
	ListenAddr        string   `json:"listen_addr"`
	CodexBin          string   `json:"codex_bin"`
	CodexMode         string   `json:"codex_mode"`
	RuntimeDir        string   `json:"runtime_dir"`
	DBPath            string   `json:"db_path"`
	AuditPath         string   `json:"audit_path"`
	AllowedRepos      []string `json:"allowed_repos"`
	AuthorizedUsers   []string `json:"authorized_users"`
	FeishuReceiveMode string   `json:"feishu_receive_mode"`
	FeishuConfigured  bool     `json:"feishu_configured"`
}

type fileConfig struct {
	ListenAddr      string   `toml:"listen_addr"`
	CodexBin        string   `toml:"codex_bin"`
	CodexMode       string   `toml:"codex_mode"`
	RuntimeDir      string   `toml:"runtime_dir"`
	DBPath          string   `toml:"db"`
	AuditPath       string   `toml:"audit_log"`
	AllowedRepos    []string `toml:"allowed_repos"`
	AuthorizedUsers []string `toml:"authorized_users"`
	Feishu          struct {
		AppID             string `toml:"app_id"`
		AppSecret         string `toml:"app_secret"`
		BaseURL           string `toml:"base_url"`
		VerificationToken string `toml:"verification_token"`
		ReceiveMode       string `toml:"receive_mode"`
	} `toml:"feishu"`
}

func Load() (Config, error) {
	cfg, err := defaultConfig()
	if err != nil {
		return Config{}, err
	}

	if value := os.Getenv("RELAYX_CONFIG"); value != "" {
		cfg.ConfigPath, err = expandPath(value)
		if err != nil {
			return Config{}, err
		}
	}

	if err := applyConfigFile(&cfg); err != nil {
		return Config{}, err
	}
	if err := applyEnv(&cfg); err != nil {
		return Config{}, err
	}
	cfg.FeishuReceiveMode = normalizeReceiveMode(cfg.FeishuReceiveMode)
	if !validReceiveMode(cfg.FeishuReceiveMode) {
		return Config{}, fmt.Errorf("invalid feishu receive mode %q", cfg.FeishuReceiveMode)
	}
	cfg.FeishuConfigured = cfg.FeishuAppID != "" && cfg.FeishuAppSecret != ""

	return cfg, nil
}

func (c Config) Summary() Summary {
	return Summary{
		ConfigPath:        c.ConfigPath,
		ListenAddr:        c.ListenAddr,
		CodexBin:          c.CodexBin,
		CodexMode:         c.CodexMode,
		RuntimeDir:        c.RuntimeDir,
		DBPath:            c.DBPath,
		AuditPath:         c.AuditPath,
		AllowedRepos:      c.AllowedRepos,
		AuthorizedUsers:   c.AuthorizedUsers,
		FeishuReceiveMode: c.FeishuReceiveMode,
		FeishuConfigured:  c.FeishuConfigured,
	}
}

func defaultConfig() (Config, error) {
	baseDir, err := defaultBaseDir()
	if err != nil {
		return Config{}, err
	}

	return Config{
		ConfigPath:        filepath.Join(baseDir, configFileName),
		ListenAddr:        defaultListenAddr,
		CodexBin:          defaultCodexBin,
		CodexMode:         defaultCodexMode,
		RuntimeDir:        filepath.Join(baseDir, "run"),
		DBPath:            filepath.Join(baseDir, "state.json"),
		AuditPath:         filepath.Join(baseDir, "logs", "audit.jsonl"),
		FeishuBaseURL:     defaultFeishuURL,
		FeishuReceiveMode: defaultFeishuMode,
	}, nil
}

func defaultBaseDir() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, appDirName), nil
}

func applyConfigFile(cfg *Config) error {
	if cfg.ConfigPath == "" {
		return nil
	}

	var fc fileConfig
	if _, err := toml.DecodeFile(cfg.ConfigPath, &fc); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load config %s: %w", cfg.ConfigPath, err)
	}

	var err error
	if fc.ListenAddr != "" {
		cfg.ListenAddr = fc.ListenAddr
	}
	if fc.CodexBin != "" {
		cfg.CodexBin = fc.CodexBin
	}
	if fc.CodexMode != "" {
		cfg.CodexMode = fc.CodexMode
	}
	if fc.RuntimeDir != "" {
		cfg.RuntimeDir, err = expandPath(fc.RuntimeDir)
		if err != nil {
			return err
		}
	}
	if fc.DBPath != "" {
		cfg.DBPath, err = expandPath(fc.DBPath)
		if err != nil {
			return err
		}
	}
	if fc.AuditPath != "" {
		cfg.AuditPath, err = expandPath(fc.AuditPath)
		if err != nil {
			return err
		}
	}
	if fc.AllowedRepos != nil {
		cfg.AllowedRepos = fc.AllowedRepos
	}
	if fc.AuthorizedUsers != nil {
		cfg.AuthorizedUsers = fc.AuthorizedUsers
	}
	if fc.Feishu.AppID != "" {
		cfg.FeishuAppID = fc.Feishu.AppID
	}
	if fc.Feishu.AppSecret != "" {
		cfg.FeishuAppSecret = fc.Feishu.AppSecret
	}
	if fc.Feishu.BaseURL != "" {
		cfg.FeishuBaseURL = fc.Feishu.BaseURL
	}
	if fc.Feishu.VerificationToken != "" {
		cfg.FeishuVerifyToken = fc.Feishu.VerificationToken
	}
	if fc.Feishu.ReceiveMode != "" {
		cfg.FeishuReceiveMode = fc.Feishu.ReceiveMode
	}

	return nil
}

func applyEnv(cfg *Config) error {
	var err error
	if value := os.Getenv("RELAYX_LISTEN_ADDR"); value != "" {
		cfg.ListenAddr = value
	}
	if value := os.Getenv("RELAYX_CODEX_BIN"); value != "" {
		cfg.CodexBin = value
	}
	if value := os.Getenv("RELAYX_CODEX_MODE"); value != "" {
		cfg.CodexMode = value
	}
	if value := os.Getenv("RELAYX_RUNTIME_DIR"); value != "" {
		cfg.RuntimeDir, err = expandPath(value)
		if err != nil {
			return err
		}
	}
	if value := os.Getenv("RELAYX_DB"); value != "" {
		cfg.DBPath, err = expandPath(value)
		if err != nil {
			return err
		}
	}
	if value := os.Getenv("RELAYX_AUDIT_LOG"); value != "" {
		cfg.AuditPath, err = expandPath(value)
		if err != nil {
			return err
		}
	}
	if value := os.Getenv("RELAYX_ALLOWED_REPOS"); value != "" {
		cfg.AllowedRepos = splitCSV(value)
	}
	if value := os.Getenv("RELAYX_AUTHORIZED_USERS"); value != "" {
		cfg.AuthorizedUsers = splitCSV(value)
	}
	if value := os.Getenv("FEISHU_APP_ID"); value != "" {
		cfg.FeishuAppID = value
	}
	if value := os.Getenv("FEISHU_APP_SECRET"); value != "" {
		cfg.FeishuAppSecret = value
	}
	if value := os.Getenv("FEISHU_BASE_URL"); value != "" {
		cfg.FeishuBaseURL = value
	}
	if value := os.Getenv("FEISHU_VERIFICATION_TOKEN"); value != "" {
		cfg.FeishuVerifyToken = value
	}
	if value := os.Getenv("FEISHU_RECEIVE_MODE"); value != "" {
		cfg.FeishuReceiveMode = value
	}
	return nil
}

func normalizeReceiveMode(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	value = strings.ReplaceAll(value, "-", "_")
	switch value {
	case "", "long", "ws", "websocket":
		return "long_connection"
	case "callback", "http", "webhook":
		return "http_callback"
	default:
		return value
	}
}

func validReceiveMode(value string) bool {
	switch value {
	case "long_connection", "http_callback", "both", "disabled":
		return true
	default:
		return false
	}
}

func expandPath(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if value == "~" {
		home, err := homeDir()
		if err != nil {
			return "", fmt.Errorf("expand path %q: %w", value, err)
		}
		return home, nil
	}
	if strings.HasPrefix(value, "~/") || strings.HasPrefix(value, `~\`) {
		home, err := homeDir()
		if err != nil {
			return "", fmt.Errorf("expand path %q: %w", value, err)
		}
		return filepath.Join(home, value[2:]), nil
	}
	return value, nil
}

func homeDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	if home == "" {
		return "", fmt.Errorf("resolve home directory: empty home")
	}
	return home, nil
}

func splitCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
