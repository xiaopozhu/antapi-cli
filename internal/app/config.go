package app

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type profile struct {
	GatewayURL     string `json:"gateway_url"`
	CoreURL        string `json:"core_url"`
	Development    bool   `json:"development"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}
type settings struct {
	DefaultProfile string             `json:"default_profile"`
	Profiles       map[string]profile `json:"profiles"`
}
type runtimeConfig struct {
	profile
	Key     string
	Timeout time.Duration
}

var errConfiguration = errors.New("configuration")

func defaultConfigPath() (string, error) {
	root := os.Getenv("XDG_CONFIG_HOME")
	if root == "" {
		var err error
		root, err = os.UserConfigDir()
		if err != nil {
			return "", errConfiguration
		}
	}
	return filepath.Join(root, "antapi", "config.json"), nil
}

func readJSON(path string, target any) error {
	f, err := os.Open(path)
	if err != nil {
		return errConfiguration
	}
	defer f.Close()
	decoder := json.NewDecoder(io.LimitReader(f, 65537))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(target); err != nil {
		return errConfiguration
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return errConfiguration
	}
	return nil
}

func loadConfig(path, name, gateway, core string) (runtimeConfig, error) {
	if path == "" {
		var err error
		path, err = defaultConfigPath()
		if err != nil {
			return runtimeConfig{}, err
		}
	}
	var config settings
	if readJSON(path, &config) != nil {
		return runtimeConfig{}, errConfiguration
	}
	if name == "" {
		name = config.DefaultProfile
	}
	selected, ok := config.Profiles[name]
	if !ok {
		return runtimeConfig{}, errConfiguration
	}
	if gateway != "" {
		selected.GatewayURL = gateway
	}
	if core != "" {
		selected.CoreURL = core
	}
	if !validOrigin(selected.GatewayURL, selected.Development) || (selected.CoreURL != "" && !validOrigin(selected.CoreURL, selected.Development)) {
		return runtimeConfig{}, errConfiguration
	}
	if selected.TimeoutSeconds == 0 {
		selected.TimeoutSeconds = 30
	}
	if selected.TimeoutSeconds < 1 || selected.TimeoutSeconds > 120 {
		return runtimeConfig{}, errConfiguration
	}
	key := os.Getenv("ANTAPI_API_KEY")
	if key == "" {
		credentialsPath := filepath.Join(filepath.Dir(path), "credentials.json")
		info, err := os.Lstat(credentialsPath)
		// Windows 没有 POSIX mode；拒绝凭据文件，要求环境注入。
		if err != nil || runtime.GOOS == "windows" || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
			return runtimeConfig{}, errConfiguration
		}
		var credentials struct {
			Profiles map[string]struct {
				APIKey string `json:"api_key"`
			} `json:"profiles"`
		}
		if readJSON(credentialsPath, &credentials) != nil {
			return runtimeConfig{}, errConfiguration
		}
		key = credentials.Profiles[name].APIKey
	}
	if key == "" || len(key) > 4096 || strings.ContainsAny(key, " \t\r\n") {
		return runtimeConfig{}, errConfiguration
	}
	return runtimeConfig{profile: selected, Key: key, Timeout: time.Duration(selected.TimeoutSeconds) * time.Second}, nil
}

func validOrigin(raw string, development bool) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" || strings.Contains(raw, "#") || (parsed.Path != "" && parsed.Path != "/") || parsed.Opaque != "" {
		return false
	}
	host := parsed.Hostname()
	if strings.Contains(host, ":") && net.ParseIP(host) == nil {
		return false
	}
	if strings.HasPrefix(parsed.Host, "[") && net.ParseIP(host) == nil {
		return false
	}
	if strings.HasSuffix(parsed.Host, ":") {
		return false
	}
	if port := parsed.Port(); port != "" {
		number, err := strconv.ParseUint(port, 10, 16)
		if err != nil || number == 0 {
			return false
		}
	}
	if parsed.Scheme == "https" {
		return true
	}
	if parsed.Scheme != "http" || !development {
		return false
	}
	ip := net.ParseIP(host)
	return strings.EqualFold(host, "localhost") || (ip != nil && ip.IsLoopback())
}
