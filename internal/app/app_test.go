package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestVersionJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	info := BuildInfo{Version: "v0.1.0", Commit: strings.Repeat("a", 40), BuiltAt: "2026-10-22T00:00:00Z"}
	if code := Run(context.Background(), []string{"antapi", "version", "--json"}, &stdout, &stderr, info); code != 0 {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
	var result map[string]string
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["version"] != info.Version || result["commit"] != info.Commit || result["built_at"] != info.BuiltAt {
		t.Fatalf("unexpected build info: %#v", result)
	}
	if stderr.Len() != 0 {
		t.Fatalf("unexpected stderr: %q", stderr.String())
	}
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) {
	return 0, errors.New("sensitive writer detail")
}

func TestOutputFailureIsRuntimeError(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"antapi", "version", "--json"}, failedWriter{}, &stderr, BuildInfo{Version: "dev"})
	if code != 1 || strings.Contains(stderr.String(), "sensitive writer detail") {
		t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
	}
}

func TestHelpNeedsNoConfiguration(t *testing.T) {
	for _, args := range [][]string{{"antapi"}, {"antapi", "--help"}, {"antapi", "version", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), args, &stdout, &stderr, BuildInfo{Version: "dev"}); code != 0 {
				t.Fatalf("exit = %d, stderr = %q", code, stderr.String())
			}
			if !strings.Contains(stdout.String(), "USAGE:") || stderr.Len() != 0 {
				t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestInvalidInvocationDoesNotEchoArguments(t *testing.T) {
	for _, args := range [][]string{
		{"antapi", "secret-value"},
		{"antapi", "--secret-value"},
		{"antapi", "version", "secret-value"},
		{"antapi", "version", "--secret-value"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), args, &stdout, &stderr, BuildInfo{Version: "dev"}); code != 2 {
				t.Fatalf("exit = %d, want 2", code)
			}
			if stdout.Len() != 0 || stderr.Len() == 0 || strings.Contains(stderr.String(), "secret-value") {
				t.Fatalf("stdout = %q, stderr = %q", stdout.String(), stderr.String())
			}
		})
	}
}
