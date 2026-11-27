package app

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDevConfig(t *testing.T, gatewayURL string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	raw := `{"default_profile":"local","profiles":{"local":{"gateway_url":"` + gatewayURL + `","development":true}}}`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestWebSearchAndFetchCommands(t *testing.T) {
	t.Setenv("ANTAPI_API_KEY", "ak-tool-test-key")
	var gotPath, gotIdem string
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Authorization") != "Bearer ak-tool-test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		gotPath = r.URL.Path
		gotIdem = r.Header.Get("Idempotency-Key")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-Request-ID", "req-web-1")
		w.Header().Set("X-AntAPI-Receipt-ID", "rcpt-web-1")
		w.Header().Set("X-AntAPI-Debit-Atoms", "5000000")
		_, _ = w.Write([]byte(`{"code":0,"data":{"items":[{"title":"AntAPI"}]}}`))
	}))
	defer server.Close()
	cfg := writeDevConfig(t, server.URL)

	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"antapi", "--config", cfg, "web", "search", "--count", "5", "--need-content", "--idempotency-key", "idem-1", "--json", "antapi docs"}, &out, &errOut, BuildInfo{Version: "test"})
	if code != 0 || gotPath != "/web/v1/search" || gotIdem != "idem-1" || gotBody["query"] != "antapi docs" || gotBody["count"] != float64(5) || gotBody["need_content"] != true {
		t.Fatalf("web search failed: code=%d path=%q idem=%q body=%v stderr=%q", code, gotPath, gotIdem, gotBody, errOut.String())
	}
	if !strings.Contains(out.String(), `"debit_atoms":"5000000"`) {
		t.Fatalf("expected debit_atoms in stdout: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	code = Run(context.Background(), []string{"antapi", "--config", cfg, "web", "fetch", "--json", "https://antapi.dev"}, &out, &errOut, BuildInfo{Version: "test"})
	if code != 0 || gotPath != "/web/v1/fetch" || gotBody["url"] != "https://antapi.dev" {
		t.Fatalf("web fetch failed: code=%d path=%q body=%v stderr=%q", code, gotPath, gotBody, errOut.String())
	}

	// 参数非法必须在联网前以退出码 2 拒绝。
	out.Reset()
	errOut.Reset()
	if code = Run(context.Background(), []string{"antapi", "web", "search", "--count", "99", "test"}, &out, &errOut, BuildInfo{Version: "test"}); code != 2 {
		t.Fatalf("invalid count must exit 2, got %d", code)
	}
	if code = Run(context.Background(), []string{"antapi", "web", "fetch", "ftp://example.com"}, &out, &errOut, BuildInfo{Version: "test"}); code != 2 {
		t.Fatalf("non-http url must exit 2, got %d", code)
	}
}

func TestOCRAndASRMultipartCommands(t *testing.T) {
	t.Setenv("ANTAPI_API_KEY", "ak-media-test-key")
	var gotPath, gotPurpose, gotFileName string
	var gotBytes []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		gotPurpose = r.FormValue("purpose")
		f, header, err := r.FormFile("file")
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		defer f.Close()
		gotFileName = header.Filename
		gotBytes, _ = io.ReadAll(f)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("X-AntAPI-Debit-Atoms", "2000000")
		_, _ = w.Write([]byte(`{"code":0,"data":{"text":"hello"}}`))
	}))
	defer server.Close()
	cfg := writeDevConfig(t, server.URL)

	sample := filepath.Join(t.TempDir(), "doc.pdf")
	if err := os.WriteFile(sample, []byte("%PDF-1.4 fixture"), 0o600); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"antapi", "--config", cfg, "ocr", "recognize", "--purpose", "rich_layout_recognition", "--json", sample}, &out, &errOut, BuildInfo{Version: "test"})
	if code != 0 || gotPath != "/ocr/v1/recognize" || gotPurpose != "rich_layout_recognition" || gotFileName != "doc.pdf" || string(gotBytes) != "%PDF-1.4 fixture" {
		t.Fatalf("ocr recognize failed: code=%d path=%q purpose=%q file=%q stderr=%q", code, gotPath, gotPurpose, gotFileName, errOut.String())
	}

	audio := filepath.Join(t.TempDir(), "clip.wav")
	if err := os.WriteFile(audio, []byte("RIFF-audio"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	errOut.Reset()
	code = Run(context.Background(), []string{"antapi", "--config", cfg, "asr", "transcribe", "--json", audio}, &out, &errOut, BuildInfo{Version: "test"})
	if code != 0 || gotPath != "/asr/v1/transcribe" || gotFileName != "clip.wav" || string(gotBytes) != "RIFF-audio" {
		t.Fatalf("asr transcribe failed: code=%d path=%q file=%q stderr=%q", code, gotPath, gotFileName, errOut.String())
	}

	// 非法 purpose 或不存在的文件在联网前返回 2。
	if code = Run(context.Background(), []string{"antapi", "ocr", "recognize", "--purpose", "bad", sample}, &out, &errOut, BuildInfo{Version: "test"}); code != 2 {
		t.Fatalf("invalid purpose must exit 2, got %d", code)
	}
	if code = Run(context.Background(), []string{"antapi", "asr", "transcribe", filepath.Join(t.TempDir(), "missing.wav")}, &out, &errOut, BuildInfo{Version: "test"}); code != 2 {
		t.Fatalf("missing audio file must exit 2, got %d", code)
	}
}

func TestTTSSynthesizeCommand(t *testing.T) {
	t.Setenv("ANTAPI_API_KEY", "ak-tts-test-key")
	failNext := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failNext {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusPaymentRequired)
			_, _ = w.Write([]byte(`{"code":4002,"error":"insufficient balance"}`))
			return
		}
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if req["voice_id"] != "tide" || req["text"] != "hello antapi" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "audio/wav")
		w.Header().Set("X-Request-ID", "req-tts-1")
		w.Header().Set("X-AntAPI-Debit-Atoms", "180000")
		_, _ = w.Write([]byte("RIFFfakewavbytes"))
	}))
	defer server.Close()
	cfg := writeDevConfig(t, server.URL)

	outDir := t.TempDir()
	wavPath := filepath.Join(outDir, "out.wav")
	var out, errOut bytes.Buffer
	code := Run(context.Background(), []string{"antapi", "--config", cfg, "tts", "synthesize", "--text", "hello antapi", "--voice", "tide", "--output", wavPath, "--json"}, &out, &errOut, BuildInfo{Version: "test"})
	if code != 0 {
		t.Fatalf("tts synthesize failed: code=%d stderr=%q", code, errOut.String())
	}
	gotWAV, err := os.ReadFile(wavPath)
	if err != nil || string(gotWAV) != "RIFFfakewavbytes" {
		t.Fatalf("unexpected wav output: %q, err=%v", string(gotWAV), err)
	}
	if !strings.Contains(out.String(), `"debit_atoms":"180000"`) || !strings.Contains(out.String(), `"bytes":16`) {
		t.Fatalf("unexpected stdout: %s", out.String())
	}

	// 平台返回 JSON 错误时，不得残留输出文件。
	failNext = true
	badPath := filepath.Join(outDir, "should-not-exist.wav")
	out.Reset()
	errOut.Reset()
	code = Run(context.Background(), []string{"antapi", "--config", cfg, "tts", "synthesize", "--text", "hello", "--output", badPath}, &out, &errOut, BuildInfo{Version: "test"})
	if code != 1 || !strings.Contains(errOut.String(), "CLI_PLATFORM_ERROR") {
		t.Fatalf("expected platform error, got code=%d stderr=%q", code, errOut.String())
	}
	if _, err := os.Stat(badPath); !os.IsNotExist(err) {
		t.Fatalf("error response must not create output wav file")
	}

	// 参数非法必须退出码 2。
	if code = Run(context.Background(), []string{"antapi", "tts", "synthesize", "--text", "hi", "--voice", "unknown", "--output", badPath}, &out, &errOut, BuildInfo{Version: "test"}); code != 2 {
		t.Fatalf("invalid voice must exit 2, got %d", code)
	}
}
