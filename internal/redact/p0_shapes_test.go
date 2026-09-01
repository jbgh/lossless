package redact

import "testing"

func TestContainsSecretModernShapes(t *testing.T) {
	yes := []string{
		"-----BEGIN PRIVATE KEY-----",
		"-----BEGIN OPENSSH PRIVATE KEY-----",
		// assembled so a push scanner does not mistake the fixture for a live hook
		"https://hooks.slack.com/" + "services/T0AB1CD2E/" + "B0FG3HI4J/" + "AbCdEfGhIjKlMnOpQrStUvWx",
		"https://discord.com/api/" + "webhooks/123456789012345678/" + "AbCdEf-GhIjKl_MnOpQrStUvWxYz0123456789",
		"redis://:Sup3rS3cret@cache.internal:6379/0",
		"amqp://guest:Sup3rS3cret@mq.internal:5672/",
		"mssql://sa:Sup3rS3cret@db.internal:1433",
		"Authorization: Basic dXNlcjpwYXNzd29yZDEyMw==",
		"gho_abcdefghijklmnopqrstuvwxyz123456",
		"ghs_abcdefghijklmnopqrstuvwxyz123456",
		"glpat-abcdefghijklmnopqrstuv",
		"ASIAIOSFODNN7EXAMPLE",
		"AWS_SECRET_ACCESS_KEY=wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		"password=Sup3rS3cretValue9",
		"api_key: 8f2a9c1d4e6b7a0f3c5d9e1b2a4c6d8e",
		"bearer abcdefghijklmnopqrstuvwxyz1234567890",
	}
	for _, s := range yes {
		if !ContainsSecret(s) {
			t.Errorf("ContainsSecret(%q) = false, want true", s)
		}
	}
	no := []string{
		"redis://localhost:6379",
		"token: jsonwebtoken",
		"the password field is required on signup",
		"set password= in .env before running",
		"Authorization: Basic auth is deprecated, use Bearer",
		"api_key: required",
		"Redis token bucket failed in src/middleware/auth.ts staging.",
	}
	for _, s := range no {
		if ContainsSecret(s) {
			t.Errorf("ContainsSecret(%q) = true, want false", s)
		}
	}
}
