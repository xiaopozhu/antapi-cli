package main

import (
	"context"
	"os"

	"github.com/xiaopozhu/antapi-cli/internal/app"
)

// 正式制品由发布流水线通过 -ldflags 注入，开发构建保持可识别的默认值。
var (
	version = "dev"
	commit  = "unknown"
	builtAt = "unknown"
)

func main() {
	os.Exit(app.Run(context.Background(), os.Args, os.Stdout, os.Stderr, app.BuildInfo{
		Version: version, Commit: commit, BuiltAt: builtAt,
	}))
}
