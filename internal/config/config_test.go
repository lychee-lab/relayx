package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestLoadDefaultsToRelayXHome(t *testing.T) {
	home := testHome(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	base := filepath.Join(home, ".relayx")
	if cfg.ConfigPath != filepath.Join(base, "config.toml") {
		t.Fatalf("ConfigPath = %q", cfg.ConfigPath)
	}
	if cfg.RuntimeDir != filepath.Join(base, "run") {
		t.Fatalf("RuntimeDir = %q", cfg.RuntimeDir)
	}
	if cfg.DBPath != filepath.Join(base, "state.json") {
		t.Fatalf("DBPath = %q", cfg.DBPath)
	}
	if cfg.AuditPath != filepath.Join(base, "logs", "audit.jsonl") {
		t.Fatalf("AuditPath = %q", cfg.AuditPath)
	}
	if cfg.FeishuConfigured {
		t.Fatal("FeishuConfigured = true, want false")
	}
}

func TestLoadReadsTOMLConfig(t *testing.T) {
	home := testHome(t)
	configPath := filepath.Join(home, "custom.toml")
	writeConfig(t, configPath, `
listen_addr = "0.0.0.0:9000"
codex_bin = "/opt/codex"
codex_mode = "app-server"
runtime_dir = "~/.relayx/custom-run"
db = "~/.relayx/custom-state.json"
audit_log = "~/.relayx/logs/custom-audit.jsonl"
authorized_users = ["ou_1", "ou_2"]
allowed_repos = ["/repo/a", "/repo/b"]

[feishu]
app_id = "cli_file"
app_secret = "secret_file"
base_url = "https://example.test/open-apis"
verification_token = "verify_file"
`)
	t.Setenv("RELAYX_CONFIG", configPath)

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ConfigPath != configPath {
		t.Fatalf("ConfigPath = %q", cfg.ConfigPath)
	}
	if cfg.ListenAddr != "0.0.0.0:9000" {
		t.Fatalf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.CodexBin != "/opt/codex" || cfg.CodexMode != "app-server" {
		t.Fatalf("codex config = %q/%q", cfg.CodexBin, cfg.CodexMode)
	}
	if cfg.RuntimeDir != filepath.Join(home, ".relayx", "custom-run") {
		t.Fatalf("RuntimeDir = %q", cfg.RuntimeDir)
	}
	if cfg.DBPath != filepath.Join(home, ".relayx", "custom-state.json") {
		t.Fatalf("DBPath = %q", cfg.DBPath)
	}
	if cfg.AuditPath != filepath.Join(home, ".relayx", "logs", "custom-audit.jsonl") {
		t.Fatalf("AuditPath = %q", cfg.AuditPath)
	}
	if !reflect.DeepEqual(cfg.AuthorizedUsers, []string{"ou_1", "ou_2"}) {
		t.Fatalf("AuthorizedUsers = %#v", cfg.AuthorizedUsers)
	}
	if !reflect.DeepEqual(cfg.AllowedRepos, []string{"/repo/a", "/repo/b"}) {
		t.Fatalf("AllowedRepos = %#v", cfg.AllowedRepos)
	}
	if cfg.FeishuAppID != "cli_file" || cfg.FeishuAppSecret != "secret_file" || cfg.FeishuBaseURL != "https://example.test/open-apis" || cfg.FeishuVerifyToken != "verify_file" {
		t.Fatalf("feishu config = %#v", cfg)
	}
	if !cfg.FeishuConfigured {
		t.Fatal("FeishuConfigured = false, want true")
	}
}

func TestLoadEnvironmentOverridesTOML(t *testing.T) {
	home := testHome(t)
	configPath := filepath.Join(home, "config.toml")
	writeConfig(t, configPath, `
listen_addr = "0.0.0.0:9000"
codex_bin = "file-codex"
codex_mode = "disabled"
runtime_dir = "~/.relayx/file-run"
db = "~/.relayx/file-state.json"
audit_log = "~/.relayx/logs/file-audit.jsonl"
authorized_users = ["ou_file"]
allowed_repos = ["/repo/file"]

[feishu]
app_id = "cli_file"
app_secret = "secret_file"
base_url = "https://file.example/open-apis"
verification_token = "verify_file"
`)
	t.Setenv("RELAYX_CONFIG", configPath)
	t.Setenv("RELAYX_LISTEN_ADDR", "127.0.0.1:9999")
	t.Setenv("RELAYX_CODEX_BIN", "env-codex")
	t.Setenv("RELAYX_CODEX_MODE", "app-server")
	t.Setenv("RELAYX_RUNTIME_DIR", "~/.relayx/env-run")
	t.Setenv("RELAYX_DB", "~/.relayx/env-state.json")
	t.Setenv("RELAYX_AUDIT_LOG", "~/.relayx/logs/env-audit.jsonl")
	t.Setenv("RELAYX_AUTHORIZED_USERS", "ou_env_a, ou_env_b")
	t.Setenv("RELAYX_ALLOWED_REPOS", "/repo/env-a,/repo/env-b")
	t.Setenv("FEISHU_APP_ID", "cli_env")
	t.Setenv("FEISHU_APP_SECRET", "secret_env")
	t.Setenv("FEISHU_BASE_URL", "https://env.example/open-apis")
	t.Setenv("FEISHU_VERIFICATION_TOKEN", "verify_env")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ListenAddr != "127.0.0.1:9999" || cfg.CodexBin != "env-codex" || cfg.CodexMode != "app-server" {
		t.Fatalf("env overrides did not apply: %#v", cfg)
	}
	if cfg.RuntimeDir != filepath.Join(home, ".relayx", "env-run") {
		t.Fatalf("RuntimeDir = %q", cfg.RuntimeDir)
	}
	if cfg.DBPath != filepath.Join(home, ".relayx", "env-state.json") {
		t.Fatalf("DBPath = %q", cfg.DBPath)
	}
	if cfg.AuditPath != filepath.Join(home, ".relayx", "logs", "env-audit.jsonl") {
		t.Fatalf("AuditPath = %q", cfg.AuditPath)
	}
	if !reflect.DeepEqual(cfg.AuthorizedUsers, []string{"ou_env_a", "ou_env_b"}) {
		t.Fatalf("AuthorizedUsers = %#v", cfg.AuthorizedUsers)
	}
	if !reflect.DeepEqual(cfg.AllowedRepos, []string{"/repo/env-a", "/repo/env-b"}) {
		t.Fatalf("AllowedRepos = %#v", cfg.AllowedRepos)
	}
	if cfg.FeishuAppID != "cli_env" || cfg.FeishuAppSecret != "secret_env" || cfg.FeishuBaseURL != "https://env.example/open-apis" || cfg.FeishuVerifyToken != "verify_env" {
		t.Fatalf("feishu env overrides did not apply: %#v", cfg)
	}
}

func TestLoadIgnoresLegacyCodexBabysitterEnv(t *testing.T) {
	home := testHome(t)
	t.Setenv("CODEX_BABYSITTER_LISTEN_ADDR", "0.0.0.0:9999")
	t.Setenv("CODEX_BABYSITTER_RUNTIME_DIR", "/tmp/legacy-run")
	t.Setenv("CODEX_BABYSITTER_DB", "/tmp/legacy-state.json")
	t.Setenv("CODEX_BABYSITTER_AUDIT_LOG", "/tmp/legacy-audit.jsonl")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}

	if cfg.ListenAddr != defaultListenAddr {
		t.Fatalf("ListenAddr = %q", cfg.ListenAddr)
	}
	if cfg.RuntimeDir != filepath.Join(home, ".relayx", "run") {
		t.Fatalf("RuntimeDir = %q", cfg.RuntimeDir)
	}
}

func TestLoadReportsInvalidTOML(t *testing.T) {
	home := testHome(t)
	configPath := filepath.Join(home, "bad.toml")
	writeConfig(t, configPath, `listen_addr = "unterminated`)
	t.Setenv("RELAYX_CONFIG", configPath)

	_, err := Load()
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "load config") {
		t.Fatalf("error = %v", err)
	}
}

func testHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("RELAYX_CONFIG", "")
	t.Setenv("RELAYX_LISTEN_ADDR", "")
	t.Setenv("RELAYX_CODEX_BIN", "")
	t.Setenv("RELAYX_CODEX_MODE", "")
	t.Setenv("RELAYX_RUNTIME_DIR", "")
	t.Setenv("RELAYX_DB", "")
	t.Setenv("RELAYX_AUDIT_LOG", "")
	t.Setenv("RELAYX_ALLOWED_REPOS", "")
	t.Setenv("RELAYX_AUTHORIZED_USERS", "")
	t.Setenv("FEISHU_APP_ID", "")
	t.Setenv("FEISHU_APP_SECRET", "")
	t.Setenv("FEISHU_BASE_URL", "")
	t.Setenv("FEISHU_VERIFICATION_TOKEN", "")
	return home
}

func writeConfig(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.TrimSpace(content)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
