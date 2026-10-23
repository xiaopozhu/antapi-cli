package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/xiaopozhu/antapi-cli/internal/i18n"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

const testKey = "ak-super-secret-test"

func setupProfile(t *testing.T, origin string, timeout int) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTAPI_API_KEY", testKey)
	config := fmt.Sprintf(`{"default_profile":"local","profiles":{"local":{"gateway_url":%q,"core_url":"https://core.example","development":true,"timeout_seconds":%d}}}`, origin, timeout)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(config), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func execute(t *testing.T, ctx context.Context, config string, args ...string) (int, string, string) {
	t.Helper()
	var out, err bytes.Buffer
	full := append([]string{"antapi", "--config", config}, args...)
	code := Run(ctx, full, &out, &err, BuildInfo{Version: "dev"})
	return code, out.String(), err.String()
}
func TestWhoisLookupJSON(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/whois/v1/domains/例子.中国" || r.Header.Get("Authorization") != "Bearer "+testKey {
			t.Errorf("unexpected request")
		}
		w.Header().Set("X-Request-ID", "req-123")
		w.Header().Set("X-AntAPI-Receipt-ID", "9007199254740993")
		w.Header().Set("X-AntAPI-Credits-Debit-Atoms", "9007199254740995")
		fmt.Fprint(w, `{"code":0,"data":{"queried_domain":"例子.中国","registered":true},"error":""}`)
	}))
	defer server.Close()
	config := setupProfile(t, server.URL, 5)
	code, out, err := execute(t, context.Background(), config, "whois", "lookup", "例子.中国", "--json")
	if code != 0 || err != "" || calls.Load() != 1 {
		t.Fatalf("code=%d out=%s err=%s calls=%d", code, out, err, calls.Load())
	}
	var result struct {
		Code     uint32         `json:"code"`
		Data     map[string]any `json:"data"`
		Metadata map[string]any `json:"metadata"`
	}
	if e := json.Unmarshal([]byte(out), &result); e != nil {
		t.Fatal(e)
	}
	if result.Data["queried_domain"] != "例子.中国" || result.Metadata["receipt_id"] != "9007199254740993" || result.Metadata["credits_debit_atoms"] != "9007199254740995" {
		t.Fatal(out)
	}
}
func TestWhoisFailuresAreSafeAndNeverRetried(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", 401, `{"code":401,"error":"` + testKey + `"}`},
		{"credits", 402, `{"code":402}`}, {"conflict", 409, `{"code":409}`}, {"limit", 429, `{"code":429}`}, {"timeout", 504, `{"code":504}`},
		{"api_error", 200, `{"code":999,"error":"` + testKey + `"}`}, {"malformed", 200, `broken ` + testKey},
		{"missing_code", 200, `{"data":{}}`}, {"trailing", 200, `{"code":0} {}`}, {"oversized", 200, strings.Repeat("x", 2<<20)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("X-Request-ID", testKey)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer s.Close()
			code, out, err := execute(t, context.Background(), setupProfile(t, s.URL, 5), "whois", "tlds", "--json")
			if code != 1 || calls.Load() != 1 || out != "" || err == "" || strings.Contains(err, testKey) {
				t.Fatalf("code=%d out=%s err=%s calls=%d", code, out, err, calls.Load())
			}
		})
	}
}
func TestWhoisRejectsRedirects(t *testing.T) {
	var leaked atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { leaked.Add(1) }))
	defer target.Close()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, target.URL, 307) }))
	defer s.Close()
	code, _, _ := execute(t, context.Background(), setupProfile(t, s.URL, 5), "whois", "tlds", "--json")
	if code != 1 || leaked.Load() != 0 {
		t.Fatalf("redirect followed: code=%d calls=%d", code, leaked.Load())
	}
}
func TestWhoisCancellationAndTimeout(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer s.Close()
	config := setupProfile(t, s.URL, 1)
	start := time.Now()
	code, _, err := execute(t, context.Background(), config, "whois", "tlds", "--json")
	if code != 1 || time.Since(start) > 3*time.Second || strings.Contains(err, testKey) {
		t.Fatal(code, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, _, _ = execute(t, ctx, config, "whois", "tlds", "--json")
	if code != 1 {
		t.Fatal(code)
	}
}
func TestInvalidInputsPrecedeConfigurationAndNetwork(t *testing.T) {
	for _, args := range [][]string{
		{"whois", "lookup"}, {"whois", "lookup", "https://example.com", "--json"}, {"whois", "lookup", "a/b", "--json"},
		{"whois", "lookup", "a\\b", "--json"}, {"whois", "lookup", "a?b", "--json"}, {"whois", "lookup", "a%2fb", "--json"},
		{"whois", "lookup", "a\nb", "--json"}, {"whois", "lookup", "localhost", "--json"}, {"whois", "lookup", "a..com", "--json"},
		{"whois", "tlds", "secret-value", "--json"}, {"whois", "batch"}, {"whois", "tlds", "--api-key", testKey},
	} {
		code, out, err := execute(t, context.Background(), "/does/not/exist", args...)
		if code != 2 || out != "" || strings.Contains(err, "secret-value") || strings.Contains(err, testKey) {
			t.Fatalf("args=%q code=%d out=%s err=%s", args, code, out, err)
		}
	}
}
func TestHelpAndVersionPrecedeConfiguration(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"--version"}, {"version", "--json"}, {"whois", "--help"}, {"whois", "lookup", "--help"}, {"whois", "tlds", "--help"}} {
		code, _, err := execute(t, context.Background(), "/does/not/exist", args...)
		if code != 0 || err != "" {
			t.Fatal(args, code, err)
		}
	}
}
func TestCredentialFileAndEnvironmentPrecedence(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX credential permissions require environment credentials on Windows")
	}
	var got string
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"code":0,"data":{"items":[]}}`)
	}))
	defer s.Close()
	config := setupProfile(t, s.URL, 5)
	credentials := filepath.Join(filepath.Dir(config), "credentials.json")
	os.WriteFile(credentials, []byte(`{"profiles":{"local":{"api_key":"file-key"}}}`), 0600)
	t.Setenv("ANTAPI_API_KEY", "")
	code, _, err := execute(t, context.Background(), config, "whois", "tlds", "--json")
	if code != 0 || got != "Bearer file-key" {
		t.Fatal(code, got, err)
	}
	os.Chmod(credentials, 0644)
	code, _, _ = execute(t, context.Background(), config, "whois", "tlds", "--json")
	if code != 1 {
		t.Fatal("insecure credentials accepted")
	}
	t.Setenv("ANTAPI_API_KEY", testKey)
	code, _, _ = execute(t, context.Background(), config, "whois", "tlds", "--json")
	if code != 0 || got != "Bearer "+testKey {
		t.Fatal(code, got)
	}
}
func TestUnsafeOriginsRejectedBeforeNetwork(t *testing.T) {
	for _, origin := range []string{"http://example.com", "https://user:secret@example.com", "https://example.com?a=b", "https://example.com#fragment", "https://example.com/path", "ftp://example.com"} {
		code, _, err := execute(t, context.Background(), setupProfile(t, origin, 5), "whois", "tlds", "--json")
		if code != 1 || strings.Contains(err, "secret") {
			t.Fatal(origin, code, err)
		}
	}
}

func TestNumericIDsAndAtomsRemainStrings(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"code":0,"data":{"id":9007199254740993,"organization_id":9007199254740995,"receipt_ids":[9007199254740997],"cost_atoms":9007199254740999}}`)
	}))
	defer s.Close()
	code, out, err := execute(t, context.Background(), setupProfile(t, s.URL, 5), "whois", "tlds", "--json")
	if code != 0 {
		t.Fatal(code, err)
	}
	var result struct {
		Data map[string]any `json:"data"`
	}
	if json.Unmarshal([]byte(out), &result) != nil {
		t.Fatal(out)
	}
	for _, name := range []string{"id", "organization_id", "cost_atoms"} {
		if _, ok := result.Data[name].(string); !ok {
			t.Fatalf("%s not string: %s", name, out)
		}
	}
}
func TestTransportErrorCannotLeakSecret(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer connection.Close()
		fmt.Fprintf(connection, "HTTP/1.1 %s invalid\r\n\r\n", testKey)
	}))
	defer s.Close()
	code, out, err := execute(t, context.Background(), setupProfile(t, s.URL, 5), "whois", "tlds", "--json")
	if code != 1 || out != "" || strings.Contains(err, testKey) || !strings.Contains(err, "CLI_NETWORK_ERROR") {
		t.Fatal(code, out, err)
	}
}
func TestInvalidArgumentsMakeZeroRequests(t *testing.T) {
	var calls atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1) }))
	defer s.Close()
	config := setupProfile(t, s.URL, 5)
	for _, args := range [][]string{{"whois", "lookup", "example.com", "extra", "--json"}, {"whois", "lookup", "a/b", "--json"}, {"whois", "tlds", "--unknown"}} {
		code, _, _ := execute(t, context.Background(), config, args...)
		if code != 2 {
			t.Fatal(code, args)
		}
	}
	if calls.Load() != 0 {
		t.Fatal("network called")
	}
}
func TestDefaultConfigAndNonsecretOverrides(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"code":0,"data":{}}`) }))
	defer s.Close()
	config := setupProfile(t, "https://unused.invalid", 5)
	root := t.TempDir()
	os.Mkdir(filepath.Join(root, ".antapi"), 0700)
	data, _ := os.ReadFile(config)
	os.WriteFile(filepath.Join(root, ".antapi", "config.json"), data, 0600)
	setHome(t, root)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"antapi", "--profile", "local", "--gateway-url", s.URL, "whois", "tlds", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 0 {
		t.Fatal(code, stderr.String())
	}
}

func TestResponseObjectKeysCannotLeakCredentials(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `{"code":0,"data":{%q:%q}}`, testKey, testKey)
	}))
	defer s.Close()
	code, out, err := execute(t, context.Background(), setupProfile(t, s.URL, 5), "whois", "tlds", "--json")
	if code != 0 || strings.Contains(out, testKey) || strings.Contains(err, testKey) {
		t.Fatal(code, out, err)
	}
}

func TestInvalidURLFlagPrecedesConfiguration(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"antapi", "--config", "/does/not/exist", "--gateway-url", "https://user:secret@example.com", "whois", "tlds", "--json"}, &stdout, &stderr, BuildInfo{})
	if code != 2 || stdout.Len() != 0 || strings.Contains(stderr.String(), "secret") {
		t.Fatal(code, stdout.String(), stderr.String())
	}
}

func TestEmptyConfigAndProfileFlagsPrecedeConfiguration(t *testing.T) {
	setHome(t, t.TempDir())
	t.Setenv("ANTAPI_API_KEY", "")
	for _, flag := range []string{"--config", "--profile"} {
		t.Run(flag, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run(context.Background(), []string{"antapi", flag, "", "whois", "tlds", "--json"}, &stdout, &stderr, BuildInfo{})
			if code != 2 || stdout.Len() != 0 || stderr.String() != i18n.InvalidInvocation+"\n" {
				t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
			}
		})
	}
}

func TestMalformedOriginFlagsPrecedeConfiguration(t *testing.T) {
	for _, origin := range []string{"https://:443", "https://example.com:", "https://example.com:0", "https://example.com:65536", "https://example.com:abc", "https://::1", "https://[example.com]"} {
		for _, flag := range []string{"--gateway-url", "--core-url"} {
			t.Run(flag+origin, func(t *testing.T) {
				var stdout, stderr bytes.Buffer
				code := Run(context.Background(), []string{"antapi", "--config", "/does/not/exist", flag, origin, "whois", "tlds", "--json"}, &stdout, &stderr, BuildInfo{})
				if code != 2 || stdout.Len() != 0 || stderr.String() != i18n.InvalidInvocation+"\n" {
					t.Fatalf("code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
				}
			})
		}
	}
}
func TestWhoisUnicodeSeparatorsPreserveOriginalPath(t *testing.T) {
	for _, domain := range []string{"例子。中国", "例子．中国", "例子｡中国", "ｅｘａｍｐｌｅ－ｔｅｓｔ.com"} {
		t.Run(domain, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				if r.URL.Path != "/whois/v1/domains/"+domain || r.URL.RawQuery != "" || r.URL.EscapedPath() != "/whois/v1/domains/"+url.PathEscape(domain) {
					t.Errorf("input path changed: %s", r.URL.String())
				}
				fmt.Fprint(w, `{"code":0,"data":{}}`)
			}))
			defer server.Close()
			code, out, err := execute(t, context.Background(), setupProfile(t, server.URL, 5), "whois", "lookup", domain, "--json")
			if code != 0 || err != "" || calls.Load() != 1 {
				t.Fatalf("code=%d stdout=%s stderr=%s calls=%d", code, out, err, calls.Load())
			}
		})
	}
}
func TestMalformedUnicodeDomainInputsPrecedeIO(t *testing.T) {
	for _, domain := range []string{"例子。。中国", "。例子中国", "例子。中国/secret", "例子。中国\x00", "例子。中 国", string([]byte{0xff}) + ".com"} {
		code, out, err := execute(t, context.Background(), "/does/not/exist", "whois", "lookup", domain, "--json")
		if code != 2 || out != "" || err != i18n.InvalidInvocation+"\n" {
			t.Fatal(code, out, err)
		}
	}
}

func TestPaidLookupNegotiatesHTTP1WhenHTTP2IsOffered(t *testing.T) {
	var calls atomic.Int32
	var negotiated atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		negotiated.Store(int32(r.ProtoMajor))
		fmt.Fprint(w, `{"code":0,"data":{}}`)
	}))
	server.EnableHTTP2 = true
	server.StartTLS()
	defer server.Close()
	template := server.Client().Transport.(*http.Transport).Clone()
	template.ForceAttemptHTTP2 = true
	template.Protocols = new(http.Protocols)
	template.Protocols.SetHTTP1(true)
	template.Protocols.SetHTTP2(true)
	original := http.DefaultTransport
	http.DefaultTransport = template
	defer func() { http.DefaultTransport = original; template.CloseIdleConnections() }()
	code, out, err := execute(t, context.Background(), setupProfile(t, server.URL, 5), "whois", "lookup", "example.com", "--json")
	if code != 0 || calls.Load() != 1 || negotiated.Load() != 1 {
		t.Fatalf("code=%d stdout=%s stderr=%s calls=%d HTTP=%d", code, out, err, calls.Load(), negotiated.Load())
	}
}
