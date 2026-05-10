package core

import "regexp"

var secretPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(sk-[a-z0-9_-]{16,})`),
	regexp.MustCompile(`(?i)(xox[baprs]-[a-z0-9-]{16,})`),
	regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16})`),
	regexp.MustCompile(`(?i)((api[_-]?key|token|secret|password)\s*[:=]\s*)("[^"]+"|'[^']+'|[^\s]+)`),
	regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`),
}

func RedactSecrets(input string) string {
	out := input
	for _, pattern := range secretPatterns {
		out = pattern.ReplaceAllStringFunc(out, func(match string) string {
			sub := pattern.FindStringSubmatch(match)
			if len(sub) >= 3 && sub[2] != "" {
				return sub[1] + "[REDACTED]"
			}
			return "[REDACTED]"
		})
	}
	return out
}
