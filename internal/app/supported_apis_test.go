package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestSupportedAPIsManifestMatchesCommands(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "supported-apis.json"))
	if err != nil {
		t.Fatalf("read supported-apis.json: %v", err)
	}
	var items []struct {
		Product  string `json:"product"`
		APIMajor string `json:"api_major"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("unmarshal supported-apis.json: %v", err)
	}
	if len(items) == 0 {
		t.Fatal("supported-apis.json must declare at least one product")
	}

	productRe := regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)
	majorRe := regexp.MustCompile(`^v[1-9][0-9]*$`)
	seen := make(map[string]struct{}, len(items))

	for _, item := range items {
		if !productRe.MatchString(item.Product) {
			t.Fatalf("invalid product slug in supported-apis.json: %q", item.Product)
		}
		if !majorRe.MatchString(item.APIMajor) {
			t.Fatalf("invalid api_major for %q: %q", item.Product, item.APIMajor)
		}
		if _, dup := seen[item.Product]; dup {
			t.Fatalf("duplicate product in supported-apis.json: %q", item.Product)
		}
		seen[item.Product] = struct{}{}

		// 每个声明支持的产品都必须有对应的 CLI 顶层子命令，防止清单与命令实现漂移。
		var out, errOut bytes.Buffer
		code := Run(context.Background(), []string{"antapi", item.Product, "--help"}, &out, &errOut, BuildInfo{Version: "test"})
		if code != 0 {
			t.Fatalf("declared product %q has no working CLI command: exit=%d stderr=%q", item.Product, code, errOut.String())
		}
	}
}
