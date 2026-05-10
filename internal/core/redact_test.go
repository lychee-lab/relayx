package core

import (
	"strings"
	"testing"
)

func TestRedactSecrets(t *testing.T) {
	input := "token=secret-value sk-1234567890abcdef1234"
	got := RedactSecrets(input)
	if strings.Contains(got, "secret-value") || strings.Contains(got, "sk-123456") {
		t.Fatalf("secret was not redacted: %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("expected redaction marker: %s", got)
	}
}
