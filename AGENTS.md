# AntAPI CLI 协作规则

先读 `README.md`，再确认主仓库的三仓库协同发布设计。

- 本仓库是本地 CLI，不是 AntAPI 的第六个后端服务；不得改变主仓库两库五应用边界。
- Go module 为 `github.com/xiaopozhu/antapi-cli`，使用 `urfave/cli/v3`。代码注释与工程文档使用中文；用户可见文案集中在 `internal/i18n/`。
- 只通过公开 HTTP 合同调用 Core API/Gateway，不访问数据库、Redis 或 Own API 内网，不导入服务端私有包。
- Help、Version、参数校验必须在读取凭据和建立网络连接前完成。
- 保持 `--json` stdout 可解析，诊断写 stderr。错误不得回显 AK、认证头、任意原始参数或响应正文。
- 只显示平台给出的金额和费用事实，不实现独立计价。超时不能推断未执行；写请求不得自动换幂等键重提。
- 正式发布由 `antapi` 总入口发起，固定源码 commit 与制品摘要，禁止覆盖已发布版本。
- 不创建实际 API 功能的空壳命令，不将未实现能力标记为可用。
- 修改后运行 `make verify`；网络及重试行为变更需要真实 HTTP 测试服务验证。
- 不提交或推送，除非用户明确要求。
- 对外 README 默认使用英文 `README.md`，并维护 `README.zh-CN.md` 中文版与双向语言链接；代码注释和其他工程文档继续使用中文。
