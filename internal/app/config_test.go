package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func setHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
}

func TestDefaultConfigUsesHomeAntapi(t *testing.T) {
	for _, xdg := range []string{"", t.TempDir()} {
		t.Run("xdg="+xdg, func(t *testing.T) {
			home := t.TempDir()
			setHome(t, home)
			t.Setenv("XDG_CONFIG_HOME", xdg)
			got, err := defaultConfigPath()
			want := filepath.Join(home, ".antapi", "config.json")
			if err != nil || got != want {
				t.Fatalf("defaultConfigPath() = %q, %v; want %q", got, err, want)
			}
		})
	}
}

func TestDefaultConfigRequiresHome(t *testing.T) {
	setHome(t, "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := defaultConfigPath(); err == nil {
		t.Fatal("missing home must not fall back to another configuration directory")
	}
}

func TestDefaultCredentialsUseHomeAntapi(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows requires environment credentials")
	}
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("ANTAPI_API_KEY", "")
	dir := filepath.Join(home, ".antapi")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"default_profile":"test","profiles":{"test":{"gateway_url":"https://gateway.example.invalid"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(`{"profiles":{"test":{"api_key":"ak-offline-home-fixture"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := loadConfig("", "", "", "")
	if err != nil || got.Key != "ak-offline-home-fixture" {
		t.Fatalf("default credentials were not loaded from home: %v", err)
	}
}

func TestOnlyKeyUsesOfficialGatewayWithoutConfigFile(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv("ANTAPI_API_KEY", "ak-offline-default-fixture")
	got, err := loadConfig("", "", "", "")
	if err != nil || got.GatewayURL != "https://api.antapi.dev" || got.Key != "ak-offline-default-fixture" || got.TimeoutSeconds != 30 {
		t.Fatalf("official defaults not applied: gateway=%q, err=%v", got.GatewayURL, err)
	}
}

func TestDefaultGatewayKeepsExplicitOverride(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv("ANTAPI_API_KEY", "ak-offline-default-fixture")
	got, err := loadConfig("", "", "https://gateway.example.invalid", "")
	if err != nil || got.GatewayURL != "https://gateway.example.invalid" {
		t.Fatalf("explicit gateway not preserved: gateway=%q, err=%v", got.GatewayURL, err)
	}
}

func TestOfficialDefaultsDoNotHideInvalidConfiguration(t *testing.T) {
	home := t.TempDir()
	setHome(t, home)
	t.Setenv("ANTAPI_API_KEY", "ak-offline-default-fixture")
	if _, err := loadConfig(filepath.Join(home, "missing.json"), "", "", ""); err == nil {
		t.Fatal("explicit missing config must fail")
	}
	if _, err := loadConfig("", "missing-profile", "", ""); err == nil {
		t.Fatal("explicit missing profile must fail")
	}
	dir := filepath.Join(home, ".antapi")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"broken":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig("", "", "", ""); err == nil {
		t.Fatal("malformed default config must fail")
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"default_profile":"test","profiles":{"test":{"gateway_url":""}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig("", "", "", ""); err == nil {
		t.Fatal("explicit empty profile gateway must fail")
	}
}
