package release

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func command(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command(args[0], args[1:]...)
	c.Dir = dir
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %v: %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func fixture(t *testing.T) (string, string, string) {
	t.Helper()
	_, source, _, _ := runtime.Caller(0)
	root := filepath.Clean(filepath.Join(filepath.Dir(source), "../.."))
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "cmd/antapi"), 0755)
	os.Mkdir(filepath.Join(dir, "scripts"), 0755)
	for _, name := range []string{"build-release.sh", "build-release.py"} {
		body, err := os.ReadFile(filepath.Join(root, "scripts", name))
		if err != nil {
			t.Fatal(err)
		}
		os.WriteFile(filepath.Join(dir, "scripts", name), body, 0755)
	}
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module fixture\ngo 1.27.2\n"), 0644)
	os.WriteFile(filepath.Join(dir, "cmd/antapi/main.go"), []byte(`package main
import("encoding/json";"os")
var version,commit,builtAt string
func main(){json.NewEncoder(os.Stdout).Encode(map[string]string{"version":version,"commit":commit,"built_at":builtAt})}
`), 0644)
	command(t, dir, "git", "init", "--quiet")
	command(t, dir, "git", "add", ".")
	command(t, dir, "git", "-c", "user.name=Fixture", "-c", "user.email=fixture@example.invalid", "-c", "commit.gpgsign=false", "-c", "core.hooksPath="+filepath.Join(dir, "no-hooks"), "commit", "--quiet", "-m", "fixture")
	sha := command(t, dir, "git", "rev-parse", "HEAD")
	return dir, sha, filepath.Join(t.TempDir(), "artifacts")
}
func TestReleaseBuilderRejectsInvalidInputsAndDirtyTrackedTree(t *testing.T) {
	dir, sha, out := fixture(t)
	for _, args := range [][]string{{"v1.2", sha, out}, {"v1.2.3", "main", out}, {"v1.2.3", strings.Repeat("a", 40), out}, {"v1.2.3", sha, filepath.Join(dir, "out")}} {
		c := exec.Command("bash", append([]string{"scripts/build-release.sh"}, args...)...)
		c.Dir = dir
		if b, err := c.CombinedOutput(); err == nil {
			t.Fatalf("accepted %v: %s", args, b)
		}
	}
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module dirty"), 0644)
	c := exec.Command("bash", "scripts/build-release.sh", "v1.2.3", sha, out)
	c.Dir = dir
	if b, err := c.CombinedOutput(); err == nil {
		t.Fatalf("dirty tree accepted: %s", b)
	}
}
func TestReleaseBuilderProducesPinnedReusableArtifacts(t *testing.T) {
	if testing.Short() {
		t.Skip("cross compilation")
	}
	dir, sha, out := fixture(t)
	command(t, dir, "bash", "scripts/build-release.sh", "v0.1.0", sha, out)
	var manifest struct {
		Version   string `json:"version"`
		Commit    string `json:"commit"`
		BuiltAt   string `json:"built_at"`
		GoVersion string `json:"go_version"`
		Artifacts []struct{ Name, SHA256, OS, Arch string }
	}
	body, err := os.ReadFile(filepath.Join(out, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if json.Unmarshal(body, &manifest) != nil || manifest.Version != "v0.1.0" || manifest.Commit != sha || len(manifest.Artifacts) != 5 || manifest.BuiltAt == "" || manifest.GoVersion != "go1.27.2" {
		t.Fatal(string(body))
	}
	checksums, err := os.ReadFile(filepath.Join(out, "SHA256SUMS"))
	if err != nil || len(strings.Split(strings.TrimSpace(string(checksums)), "\n")) != 5 {
		t.Fatal(string(checksums), err)
	}
	// 重复准备只验证并复用既有制品，不重新编译。
	command(t, dir, "bash", "scripts/build-release.sh", "v0.1.0", sha, out)
	after, _ := os.ReadFile(filepath.Join(out, "manifest.json"))
	if string(body) != string(after) {
		t.Fatal("manifest changed")
	}
	// 验证归档中实际二进制的编译信息。
	archive := filepath.Join(out, "antapi_v0.1.0_"+runtime.GOOS+"_"+runtime.GOARCH+".tar.gz")
	if runtime.GOOS != "windows" {
		f, err := os.Open(archive)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		defer gz.Close()
		reader := tar.NewReader(gz)
		header, err := reader.Next()
		if err != nil || header.Name != "antapi" {
			t.Fatal(header, err)
		}
		binary := filepath.Join(t.TempDir(), "antapi")
		b, err := os.Create(binary)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = b.ReadFrom(reader); err != nil {
			t.Fatal(err)
		}
		b.Close()
		os.Chmod(binary, 0755)
		result := command(t, dir, binary)
		if !strings.Contains(result, sha) || !strings.Contains(result, "v0.1.0") {
			t.Fatal(result)
		}
	}
	second := filepath.Join(t.TempDir(), "deterministic")
	os.WriteFile(filepath.Join(dir, "cmd/antapi/untracked.go"), []byte("invalid untracked source"), 0644)
	command(t, dir, "bash", "scripts/build-release.sh", "v0.1.0", sha, second)
	again, _ := os.ReadFile(filepath.Join(second, "manifest.json"))
	if string(body) != string(again) {
		t.Fatal("fresh candidate differs")
	}
	first := filepath.Join(out, manifest.Artifacts[0].Name)
	os.WriteFile(first, []byte("tamper"), 0644)
	c := exec.Command("bash", "scripts/build-release.sh", "v0.1.0", sha, out)
	c.Dir = dir
	if b, err := c.CombinedOutput(); err == nil {
		t.Fatalf("tampered candidate reused: %s", b)
	}
}
