package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestSetupNeedsNoCredentialsOrConfiguration(t *testing.T) {
	setHome(t, "")
	t.Setenv("ANTAPI_API_KEY", "fixture-secret-never-display")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"antapi", "setup"}, &stdout, &stderr, BuildInfo{})
	if code != 0 || stderr.Len() != 0 || !strings.Contains(stdout.String(), "antapi doctor") || !strings.Contains(stdout.String(), "https://api.antapi.dev") || strings.Contains(stdout.String(), "fixture-secret") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestDoctorChecksOnlyLocalConfiguration(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv("ANTAPI_API_KEY", "fixture-secret-never-display")
	var stdout, stderr bytes.Buffer
	// .invalid 不可达；doctor 不应建立连接或验证远端 Key。
	code := Run(context.Background(), []string{"antapi", "--gateway-url", "https://gateway.example.invalid", "doctor", "--json"}, &stdout, &stderr, BuildInfo{})
	var result struct {
		GatewayURL     string `json:"gateway_url"`
		KeyConfigured  bool   `json:"key_configured"`
		NetworkChecked bool   `json:"network_checked"`
	}
	if code != 0 || stderr.Len() != 0 || json.Unmarshal(stdout.Bytes(), &result) != nil || result.GatewayURL != "https://gateway.example.invalid" || !result.KeyConfigured || result.NetworkChecked || strings.Contains(stdout.String(), "fixture-secret") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestDoctorMissingKeyExplainsSetupWithoutLeaking(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv("ANTAPI_API_KEY", "")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"antapi", "doctor", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 1 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "antapi setup") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestOnboardingRejectsArgumentsAndEmptyOverrides(t *testing.T) {
	for _, args := range [][]string{{"setup", "secret-value"}, {"doctor", "secret-value"}, {"--profile", "", "doctor"}, {"--config", "", "doctor"}} {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), append([]string{"antapi"}, args...), &stdout, &stderr, BuildInfo{})
		if code != 2 || stdout.Len() != 0 || strings.Contains(stderr.String(), "secret-value") {
			t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
		}
	}
}

func TestNewUserHelpMentionsSetup(t *testing.T) {
	setHome(t, "")
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"antapi"}, &stdout, &stderr, BuildInfo{}); code != 0 || !strings.Contains(stdout.String(), "antapi setup") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestDoctorRedactsCredentialEchoInGateway(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv("ANTAPI_API_KEY", "fixture-secret")
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"antapi", "--gateway-url", "https://fixture-secret.example.invalid", "doctor", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 || strings.Contains(stdout.String()+stderr.String(), "fixture-secret") || !strings.Contains(stdout.String(), "[redacted]") {
		t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}
