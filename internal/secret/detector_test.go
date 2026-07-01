package secret

import (
	"strings"
	"testing"
)

func TestSanitizeReplacesAPIKey(t *testing.T) {
	input := "use token sk-test-redacted-example for local testing"

	output := Sanitize(input, deterministicRef)

	if strings.Contains(output.Text, "sk-test-redacted-example") {
		t.Fatal("sanitized text leaked api key")
	}
	if !strings.Contains(output.Text, "secret_ref:secret_1") {
		t.Fatalf("sanitized text missing secret ref: %q", output.Text)
	}
	if len(output.Secrets) != 1 {
		t.Fatalf("secrets len = %d, want 1", len(output.Secrets))
	}
}

func TestSanitizeReplacesPasswordAssignment(t *testing.T) {
	input := "password = password-test-redacted"

	output := Sanitize(input, deterministicRef)

	if strings.Contains(output.Text, "password-test-redacted") {
		t.Fatal("sanitized text leaked password")
	}
	if len(output.Secrets) != 1 {
		t.Fatalf("secrets len = %d, want 1", len(output.Secrets))
	}
}

func TestSanitizeReplacesPrivateKeyBlock(t *testing.T) {
	input := "-----BEGIN PRIVATE KEY-----\\nfake-test-redacted\\n-----END PRIVATE KEY-----"

	output := Sanitize(input, deterministicRef)

	if strings.Contains(output.Text, "fake-test-redacted") {
		t.Fatal("sanitized text leaked private key content")
	}
	if len(output.Secrets) != 1 {
		t.Fatalf("secrets len = %d, want 1", len(output.Secrets))
	}
}

func TestSanitizeLeavesNormalText(t *testing.T) {
	input := "normal deployment note without credentials"

	output := Sanitize(input, deterministicRef)

	if output.Text != input {
		t.Fatalf("text changed = %q, want original", output.Text)
	}
	if len(output.Secrets) != 0 {
		t.Fatalf("secrets len = %d, want 0", len(output.Secrets))
	}
}

func deterministicRef(index int, match string) string {
	return "secret_" + string(rune('0'+index))
}
