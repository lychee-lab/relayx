package core

import (
	"fmt"
	"path/filepath"
	"strings"
)

type Policy struct {
	AuthorizedUsers []string
	AllowedRepos    []string
}

func (p Policy) Authorize(userID, repo string) error {
	if len(p.AuthorizedUsers) > 0 && !contains(p.AuthorizedUsers, userID) {
		return fmt.Errorf("user %q is not authorized", userID)
	}
	if repo != "" && len(p.AllowedRepos) > 0 && !pathAllowed(repo, p.AllowedRepos) {
		return fmt.Errorf("repo %q is not allowed", repo)
	}
	return nil
}

type CommandRisk struct {
	Level        string `json:"level"`
	Reason       string `json:"reason"`
	SessionGrant bool   `json:"session_grant"`
}

func AssessCommandRisk(command string) CommandRisk {
	normalized := strings.ToLower(strings.TrimSpace(command))
	highRisk := []string{
		"rm -rf",
		"git reset --hard",
		"git clean -fd",
		"sudo ",
		"chmod -r",
		"curl | sh",
		"wget | sh",
		"danger-full-access",
	}
	for _, marker := range highRisk {
		if strings.Contains(normalized, marker) {
			return CommandRisk{Level: "high", Reason: marker, SessionGrant: false}
		}
	}
	if strings.Contains(normalized, "pip install") || strings.Contains(normalized, "npm install") || strings.Contains(normalized, "cargo install") {
		return CommandRisk{Level: "medium", Reason: "dependency install", SessionGrant: true}
	}
	return CommandRisk{Level: "low", Reason: "default", SessionGrant: true}
}

func contains(items []string, value string) bool {
	for _, item := range items {
		if item == value {
			return true
		}
	}
	return false
}

func pathAllowed(repo string, allowed []string) bool {
	cleanRepo, err := filepath.Abs(filepath.Clean(repo))
	if err != nil {
		return false
	}
	for _, root := range allowed {
		cleanRoot, err := filepath.Abs(filepath.Clean(root))
		if err != nil {
			continue
		}
		if cleanRepo == cleanRoot || strings.HasPrefix(cleanRepo, cleanRoot+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
