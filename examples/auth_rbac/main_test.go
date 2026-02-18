package main

import (
	"os"
	"testing"
)

func TestMustReadDemoEnv(t *testing.T) {
	t.Setenv("GINX_TEST_ENV", "safe-value")
	v, err := mustReadDemoEnv("GINX_TEST_ENV")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if v != "safe-value" {
		t.Fatalf("expected safe-value, got %q", v)
	}

	t.Setenv("GINX_TEST_ENV_EMPTY", "")
	if _, err := mustReadDemoEnv("GINX_TEST_ENV_EMPTY"); err == nil {
		t.Fatalf("expected error for empty env")
	}

	t.Setenv("GINX_TEST_ENV_INSECURE", "changeme")
	if _, err := mustReadDemoEnv("GINX_TEST_ENV_INSECURE"); err == nil {
		t.Fatalf("expected error for insecure env value")
	}
}

func TestLoadDemoCredentialsFromEnv(t *testing.T) {
	t.Setenv(envJWTSecret, "very-strong-secret")
	t.Setenv(envAdminPassword, "admin-pass-123")
	t.Setenv(envUserPassword, "user-pass-123")

	creds, err := loadDemoCredentialsFromEnv()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if creds.jwtSecret == "" || creds.adminPassword == "" || creds.userPassword == "" {
		t.Fatalf("expected non-empty credentials")
	}
}

func TestMustReadDemoEnv_MissingVar(t *testing.T) {
	_ = os.Unsetenv("GINX_TEST_MISSING_ENV")
	if _, err := mustReadDemoEnv("GINX_TEST_MISSING_ENV"); err == nil {
		t.Fatalf("expected error for missing env")
	}
}
