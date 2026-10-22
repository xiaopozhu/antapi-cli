# AntAPI CLI

[English](README.md) | [简体中文](README.zh-CN.md)

AntAPI 官方本地客户端，供终端用户、自动化脚本和 `antapi-skills` 使用。平台 AK 与 Credits 沿用 Gateway 合同，不访问 Core 私有接口、数据库或 Own API 内网。

## 当前状态

已实现 WHOIS 单域名查询、TLD 覆盖列表、帮助、版本和固定源码候选制品构建。首个预期版本为 `v0.1.0`，目前尚未正式发布，不提供已发布安装命令。开发构建显示 `dev`；未实现批量任务、登录或自动重试。

## 使用

```bash
antapi --help
antapi version --json
antapi --profile local whois lookup example.com --json
antapi --profile local whois lookup 例子.中国 --json
antapi --profile local whois tlds --json
```

`--config PATH`、`--profile NAME`、`--gateway-url ORIGIN`、`--core-url ORIGIN` 为非敏感配置覆盖。推荐放在 `whois` 前。AK 不接受命令参数；通过私密运行环境注入 `ANTAPI_API_KEY`，不要把真实值放入 shell 历史。

查询只访问 Gateway 的 `/whois/v1/domains/{domain}`、`/whois/v1/tlds`，不调用 Core。`core_url` 仅保留集中配置位置供后续公开 Catalog 兼容核验；不会向 Core 发送 AK。

域名支持 Unicode/Punycode 与 IDNA 的 Unicode 点分隔符（`。`、`．`、`｡`）；请求保留原始 Unicode 路径，由公开 API 执行完整 IDNA 和注册域校验。CLI 只检查路径安全与裸域名结构；拒绝协议、路径、空 label、空格、控制字符、端口、百分号和 query 等不安全输入。Help、Version 和参数错误先于配置、凭据与网络。

## 配置和凭据

配置默认在 `$XDG_CONFIG_HOME/antapi/config.json`；未设置时使用 Go `os.UserConfigDir()` 下的 `antapi/config.json`：Linux 通常为 `~/.config`，macOS 为 `~/Library/Application Support`，Windows 为 `%AppData%`。`--config` 可覆盖路径。示例：

```json
{
  "default_profile": "local",
  "profiles": {
    "local": {
      "gateway_url": "http://127.0.0.1:9000",
      "core_url": "http://127.0.0.1:9001",
      "development": true,
      "timeout_seconds": 30
    }
  }
}
```

正式 profile 使用 HTTPS origin，不内嵌未经核实的生产域名。HTTP 仅在显式 `development: true` 且 hostname 为 localhost 或 loopback IP 时允许。Origin 必须有 hostname，显式端口范围为 1–65535，不含用户信息、路径（单独 `/` 可用）、query 或 fragment。请求不跟随重定向，30 秒默认超时（允许 1–120 秒），响应上限 1 MiB；收费与免费请求均不自动重试；Transport 显式限制为 HTTP/1 并禁用连接复用，避免 HTTP/2 REFUSED_STREAM 等透明重发。超时或断开不能推断未执行，需从 Console 检查平台记录。

`ANTAPI_API_KEY` 优先于配置文件同目录的 `credentials.json`；凭据文件必须为常规文件且权限精确为 `0600`，不接受符号链接。格式如下，示例占位符必须替换为本人授权的 AK，文件不得提交：

```json
{"profiles":{"local":{"api_key":"<platform-ak>"}}}
```

Windows 不支持此 POSIX 文件权限合同，因此必须使用环境凭据。配置与凭据解析均集中在 `internal/app/config.go`，不记录任何配置值或响应正文。

## 输出协议

`--json` 的 stdout 只输出一个 JSON 对象；目前省略 `--json` 时也输出同样结构。成功必须同时满足 HTTP 2xx 与平台 `code: 0`：

```json
{"code":0,"data":{"domain":{"queried":"example.com"},"registration":{"registered":true}},"metadata":{"http_status":200,"request_id":"req-123","receipt_id":"123","credits_debit_atoms":"100"}}
```

`data` 保留公开 API 结果；平台 ID、`*_id`/`*_ids` 与 `*_atoms` 均使用字符串，避免精度损失。metadata 仅提取允许的 Request ID、Receipt ID、Credits atoms，缺失时省略；CLI 不计算金额。不输出服务端 `error` 正文。

失败 stdout 为空，stderr 使用稳定的 `CLI_CONFIGURATION_ERROR`、`CLI_NETWORK_ERROR`、`CLI_INVALID_RESPONSE`、`CLI_PLATFORM_ERROR` 或 `CLI_EXECUTION_FAILED`，可附平台数值 `api_code` 与安全 Request ID。底层异常、原始参数和响应正文不会进入诊断。退出码：0 成功；1 配置、网络或平台错误；2 参数错误。

## 开发与候选制品

依赖 Go 1.27.2、Make；发布构建还需要 Git、Bash 与 Python 3.9+。使用 `urfave/cli/v3`，不导入服务端私有 Go 包。用户可见文案集中在 `internal/i18n/`；注释和工程文档使用中文，默认 README 使用英文并提供中文版。

```bash
make verify
make build
bash scripts/build-release.sh v0.1.0 <40位源码commit> /absolute/path/outside-checkout/candidate
```

`make verify` 执行格式、测试（含本地 HTTP 服务与真实跨平台构建 fixture）、vet 和编译。`go test -short ./...` 仅跳过耗时的五目标跨编译测试。测试不访问生产 API，不创建真实仓库提交；发布测试只在临时 fixture 初始化 Git。

构建器要求当前 HEAD 为目标 40 位小写 SHA、tracked 文件干净、版本为规范 `vX.Y.Z`，输出目录位于 checkout 之外。构建使用该 commit 的 `git archive`，未跟踪文件无法混入；固定 `CGO_ENABLED=0`、Go 1.27.2、`-trimpath` 与 commit UTC 时间，编译信息注入 `main.version`、`main.commit`、`main.builtAt`。不发布 tag/Release。

目标为 darwin/linux 的 amd64/arm64 与 windows/amd64：

```text
candidate/
  antapi_v0.1.0_darwin_amd64.tar.gz
  antapi_v0.1.0_darwin_arm64.tar.gz
  antapi_v0.1.0_linux_amd64.tar.gz
  antapi_v0.1.0_linux_arm64.tar.gz
  antapi_v0.1.0_windows_amd64.zip
  SHA256SUMS
  manifest.json
```

Tar 根成员为 `antapi`，ZIP 根成员为 `antapi.exe`。`SHA256SUMS` 包含五个 archive 摘要；manifest 包含 `schema_version`、`version`、`commit`、`built_at`、`go_version` 与 `artifacts:[{name,sha256,os,arch}]`。再次运行相同输入只核验并复用既有附件，不重新编译；任何摘要、输入或文件集合冲突均拒绝。协调发布直接使用此 candidate，发布阶段不得重建。

## 协同发布

三个仓库独立版本，由 `antapi-skills` 的 `antapi-release` 维护 Skill 以冻结发布包协调：客户端候选或已验证兼容的正式版本 → 手动 Admin 生效。新接口尚未生效时默认 Draft Release；Docker 部署由操作者另外完成，Catalog 发布后重建官网。WHOIS Skill 依赖 CLI `>=0.1.0 <1.0.0`，尚非已发布声明。CI 只验证，不在 push/tag 时自动发布。

- 操作约定：主仓库 `docs/ops/api-release-skill.md`（本地实现进入远端后可从远端查阅）
