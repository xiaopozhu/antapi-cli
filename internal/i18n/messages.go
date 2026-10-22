// Package i18n 集中管理 CLI 的用户可见文案。
package i18n

const (
	Redacted           = "[redacted]"
	AppUsage           = "AntAPI command-line client"
	VersionUsage       = "Show version and build information"
	VersionFormat      = "antapi %s (commit %s, built %s)\n"
	JSONUsage          = "Print machine-readable JSON"
	InvalidInvocation  = "Invalid command or options. Run antapi --help."
	ExecutionFailed    = "CLI_EXECUTION_FAILED"
	ConfigUsage        = "Read nonsecret profile configuration from this file"
	ProfileUsage       = "Select a configured profile"
	GatewayUsage       = "Override Gateway origin"
	CoreUsage          = "Override public Core origin"
	WhoisUsage         = "Query WHOIS registration and TLD coverage"
	LookupUsage        = "Look up one domain through Gateway"
	TLDUsage           = "List WHOIS TLD coverage through Gateway"
	DomainArgument     = "<domain>"
	ConfigurationError = "CLI_CONFIGURATION_ERROR"
	NetworkError       = "CLI_NETWORK_ERROR"
	ResponseError      = "CLI_INVALID_RESPONSE"
	PlatformError      = "CLI_PLATFORM_ERROR"
	APICodeFormat      = " api_code=%d"
	RequestIDFormat    = " request_id=%s"
)
