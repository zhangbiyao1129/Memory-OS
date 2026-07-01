package secret

import (
	"regexp"
)

type DetectedSecret struct {
	Ref   string
	Kind  string
	Value string
}

type SanitizedText struct {
	Text    string
	Secrets []DetectedSecret
}

type RefFactory func(index int, match string) string

var secretPatterns = []struct {
	kind    string
	pattern *regexp.Regexp
}{
	{kind: "private_key", pattern: regexp.MustCompile(`(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`)},
	{kind: "api_key", pattern: regexp.MustCompile(`\b(?:sk|pk)[_-][A-Za-z0-9_-]{8,}\b`)},
	{kind: "password", pattern: regexp.MustCompile(`(?i)\bpassword\s*[:=]\s*[^\s]+`)},
}

func Sanitize(input string, makeRef RefFactory) SanitizedText {
	if makeRef == nil {
		makeRef = func(index int, match string) string { return "secret_ref" }
	}

	result := SanitizedText{Text: input}
	for _, item := range secretPatterns {
		result.Text = item.pattern.ReplaceAllStringFunc(result.Text, func(match string) string {
			ref := makeRef(len(result.Secrets)+1, match)
			result.Secrets = append(result.Secrets, DetectedSecret{Ref: ref, Kind: item.kind, Value: match})
			return "secret_ref:" + ref
		})
	}
	return result
}
