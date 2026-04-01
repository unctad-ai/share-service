package main

import "regexp"

// SecretFinding represents a detected secret pattern in content.
type SecretFinding struct {
	Pattern string
}

type secretPattern struct {
	label string
	re    *regexp.Regexp
}

var secretPatterns = []secretPattern{
	{
		label: "AWS Access Key",
		re:    regexp.MustCompile(`AKIA[0-9A-Z]{16}`),
	},
	{
		label: "AWS Secret Key",
		re:    regexp.MustCompile(`(?i)aws[_\-]?secret[_\-]?access[_\-]?key\s*[=:]\s*\S+`),
	},
	{
		label: "Private Key PEM",
		re:    regexp.MustCompile(`-----BEGIN\s+[\w\s]*PRIVATE KEY-----`),
	},
	{
		label: "GitHub Token",
		re:    regexp.MustCompile(`gh[ps]_[A-Za-z0-9_]{36,}`),
	},
	{
		label: "GitHub PAT",
		re:    regexp.MustCompile(`github_pat_[A-Za-z0-9_]{22,}`),
	},
	{
		label: "Slack Token",
		re:    regexp.MustCompile(`xox[bporas]-[0-9A-Za-z\-]+`),
	},
	{
		label: "Slack Webhook",
		re:    regexp.MustCompile(`hooks\.slack\.com/services/T[A-Z0-9]+/B[A-Z0-9]+/[A-Za-z0-9]+`),
	},
	{
		label: "Generic API Key",
		re:    regexp.MustCompile(`(?i)["']?(?:api[_\-]?key|apikey)["']?\s*[:=]\s*["']?\S{10,}["']?`),
	},
	{
		label: "Generic Secret",
		re:    regexp.MustCompile(`(?i)["']?(?:secret[_\-]?key|client[_\-]?secret)["']?\s*[:=]\s*["']?\S{10,}["']?`),
	},
	{
		label: "Password",
		re:    regexp.MustCompile(`(?i)["']?password["']?\s*[:=]\s*["'][^"']{1,}["']`),
	},
	{
		label: "Bearer JWT",
		re:    regexp.MustCompile(`Bearer\s+eyJ[A-Za-z0-9_-]{20,}\.[A-Za-z0-9_-]{20,}`),
	},
}

// ScanSecrets scans content for known secret patterns and returns a list of findings.
func ScanSecrets(content string) []SecretFinding {
	var findings []SecretFinding
	for _, p := range secretPatterns {
		if p.re.MatchString(content) {
			findings = append(findings, SecretFinding{Pattern: p.label})
		}
	}
	return findings
}
