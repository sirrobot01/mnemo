package safety

import (
	"fmt"
	"regexp"
	"strings"
)

type Finding struct {
	Type   string
	Reason string
}

var secretPatterns = []struct {
	name    string
	pattern *regexp.Regexp
}{
	{name: "private_key", pattern: regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{name: "api_token", pattern: regexp.MustCompile(`(?i)(api[_-]?key|token|secret)\s*[:=]\s*['"]?[A-Za-z0-9_\-]{16,}`)},
	{name: "password", pattern: regexp.MustCompile(`(?i)password\s*[:=]\s*['"]?[^'"\s]{8,}`)},
	{name: "github_token", pattern: regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{20,}`)},
	{name: "aws_access_key", pattern: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
}

func DetectSecrets(content string) []Finding {
	var findings []Finding
	for _, candidate := range secretPatterns {
		if candidate.pattern.MatchString(content) {
			findings = append(findings, Finding{
				Type:   candidate.name,
				Reason: fmt.Sprintf("content matches %s secret pattern", candidate.name),
			})
		}
	}
	return findings
}

func RejectIfSecret(content string) error {
	findings := DetectSecrets(content)
	if len(findings) == 0 {
		return nil
	}
	reasons := make([]string, 0, len(findings))
	for _, finding := range findings {
		reasons = append(reasons, finding.Reason)
	}
	return fmt.Errorf("refusing to store likely secret: %s", strings.Join(reasons, "; "))
}
