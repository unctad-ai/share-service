package main

import (
	"testing"
)

func TestScanSecrets(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		wantFound bool
	}{
		// Clean content — no findings
		{
			name:      "clean HTML",
			content:   "<h1>Hello World</h1><p>This is safe content.</p>",
			wantFound: false,
		},
		{
			name:      "clean markdown",
			content:   "# Title\n\nThis is a paragraph with no secrets.\n\n## Section\n\nMore text here.",
			wantFound: false,
		},

		// AWS credentials
		{
			name:      "AWS access key",
			content:   "AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE",
			wantFound: true,
		},
		{
			name:      "AWS secret key",
			content:   "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
			wantFound: true,
		},

		// Private keys
		{
			name:      "RSA private key PEM",
			content:   "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEA...\n-----END RSA PRIVATE KEY-----",
			wantFound: true,
		},
		{
			name:      "EC private key PEM",
			content:   "-----BEGIN EC PRIVATE KEY-----\nMHQCAQEEI...\n-----END EC PRIVATE KEY-----",
			wantFound: true,
		},
		{
			name:      "generic private key PEM",
			content:   "-----BEGIN PRIVATE KEY-----\nMIIEvAIBADANBgkqhkiG...\n-----END PRIVATE KEY-----",
			wantFound: true,
		},

		// GitHub tokens
		{
			name:      "GitHub token ghp_",
			content:   "token: ghp_16C7e42F292c6912E7710c838347Ae178B4a",
			wantFound: true,
		},
		{
			name:      "GitHub token ghs_",
			content:   "authorization: ghs_16C7e42F292c6912E7710c838347Ae178B4a",
			wantFound: true,
		},
		{
			name:      "GitHub PAT",
			content:   "github_pat_11ABCDEFG0123456789_abcdefghijklmnopqrstuvwxyz",
			wantFound: true,
		},

		// Slack tokens
		{
			name:      "Slack bot token",
			content:   "SLACK_TOKEN=xoxb-123456789012-123456789012-abc123def456ghi789jkl",
			wantFound: true,
		},
		{
			name:      "Slack webhook",
			content:   "webhook: https://hooks.slack.com/services/T00000000/B00000000/XXXXXXXXXXXXXXXXXXXXXXXX",
			wantFound: true,
		},

		// Generic API keys
		{
			name:      "generic api_key",
			content:   `{"api_key": "supersecretapikey123456"}`,
			wantFound: true,
		},
		{
			name:      "generic apikey",
			content:   `apikey=abcdefghijklmnopqrstuvwxyz123456`,
			wantFound: true,
		},

		// Generic secrets
		{
			name:      "secret_key",
			content:   `{"secret_key": "my-super-secret-value-here"}`,
			wantFound: true,
		},
		{
			name:      "client_secret",
			content:   `client_secret = abcdefghijklmnopqrst`,
			wantFound: true,
		},

		// Passwords
		{
			name:      "password in JSON config",
			content:   `{"password": "hunter2"}`,
			wantFound: true,
		},
		{
			name:      "password in INI config",
			content:   `password = "my_db_password"`,
			wantFound: true,
		},

		// Bearer JWT
		{
			name:      "Bearer JWT token",
			content:   "Authorization: Bearer eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIn0",
			wantFound: true,
		},

		// False positives — should NOT trigger
		{
			name:      "base64 image data",
			content:   `<img src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg==">`,
			wantFound: false,
		},
		{
			name:      "empty password value",
			content:   `{"password": ""}`,
			wantFound: false,
		},
		{
			name:      "password mentioned in prose",
			content:   "Remember to use a strong password for your account. Never share your password with others.",
			wantFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			findings := ScanSecrets(tt.content)
			if tt.wantFound && len(findings) == 0 {
				t.Errorf("expected secret to be detected in: %q", tt.content)
			}
			if !tt.wantFound && len(findings) > 0 {
				t.Errorf("expected no findings but got %d: %v for content: %q", len(findings), findings, tt.content)
			}
		})
	}
}
