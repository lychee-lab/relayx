package config

import (
	"os"
	"strings"
)

type Config struct {
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
	FeishuConfigured  bool
}

type Summary struct {
	ListenAddr       string   `json:"listen_addr"`
	CodexBin         string   `json:"codex_bin"`
	CodexMode        string   `json:"codex_mode"`
	RuntimeDir       string   `json:"runtime_dir"`
	DBPath           string   `json:"db_path"`
	AuditPath        string   `json:"audit_path"`
	AllowedRepos     []string `json:"allowed_repos"`
	AuthorizedUsers  []string `json:"authorized_users"`
	FeishuConfigured bool     `json:"feishu_configured"`
}

func LoadFromEnv() Config {
	return Config{
		ListenAddr:        getenvCompat("RELAYX_LISTEN_ADDR", "CODEX_BABYSITTER_LISTEN_ADDR", "127.0.0.1:8787"),
		CodexBin:          getenvCompat("RELAYX_CODEX_BIN", "CODEX_BABYSITTER_CODEX_BIN", "codex"),
		CodexMode:         getenvCompat("RELAYX_CODEX_MODE", "CODEX_BABYSITTER_CODEX_MODE", "disabled"),
		RuntimeDir:        getenvCompat("RELAYX_RUNTIME_DIR", "CODEX_BABYSITTER_RUNTIME_DIR", ".relayx/run"),
		DBPath:            getenvCompat("RELAYX_DB", "CODEX_BABYSITTER_DB", ".relayx/state.json"),
		AuditPath:         getenvCompat("RELAYX_AUDIT_LOG", "CODEX_BABYSITTER_AUDIT_LOG", ".relayx/audit.jsonl"),
		AllowedRepos:      splitCSV(getenvCompat("RELAYX_ALLOWED_REPOS", "CODEX_BABYSITTER_ALLOWED_REPOS", "")),
		AuthorizedUsers:   splitCSV(getenvCompat("RELAYX_AUTHORIZED_USERS", "CODEX_BABYSITTER_AUTHORIZED_USERS", "")),
		FeishuAppID:       os.Getenv("FEISHU_APP_ID"),
		FeishuAppSecret:   os.Getenv("FEISHU_APP_SECRET"),
		FeishuBaseURL:     getenv("FEISHU_BASE_URL", "https://open.feishu.cn/open-apis"),
		FeishuVerifyToken: os.Getenv("FEISHU_VERIFICATION_TOKEN"),
		FeishuConfigured:  os.Getenv("FEISHU_APP_ID") != "" && os.Getenv("FEISHU_APP_SECRET") != "",
	}
}

func (c Config) Summary() Summary {
	return Summary{
		ListenAddr:       c.ListenAddr,
		CodexBin:         c.CodexBin,
		CodexMode:        c.CodexMode,
		RuntimeDir:       c.RuntimeDir,
		DBPath:           c.DBPath,
		AuditPath:        c.AuditPath,
		AllowedRepos:     c.AllowedRepos,
		AuthorizedUsers:  c.AuthorizedUsers,
		FeishuConfigured: c.FeishuConfigured,
	}
}

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if value == "" {
		return fallback
	}
	return value
}

func getenvCompat(primary, legacy, fallback string) string {
	if value := os.Getenv(primary); value != "" {
		return value
	}
	if value := os.Getenv(legacy); value != "" {
		return value
	}
	return fallback
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
