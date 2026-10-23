package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"runtime"
	"strings"

	"github.com/urfave/cli/v3"
	"github.com/xiaopozhu/antapi-cli/internal/i18n"
)

// onboardingCommands 仅提供本机引导与配置检查，不读取终端输入、不写文件或调用 API。
func onboardingCommands(stdout, stderr io.Writer, usageError cli.OnUsageErrorFunc) []*cli.Command {
	return []*cli.Command{{
		Name: "setup", Usage: i18n.SetupUsage, OnUsageError: usageError,
		Action: func(_ context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return cli.Exit(i18n.InvalidInvocation, 2)
			}
			input := i18n.SetupShell
			if runtime.GOOS == "windows" {
				input = i18n.SetupPowerShell
			}
			_, err := fmt.Fprintf(stdout, i18n.SetupGuide, defaultGatewayURL, input)
			return err
		},
	}, {
		Name: "doctor", Usage: i18n.DoctorUsage, OnUsageError: usageError,
		Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: i18n.JSONUsage, OnlyOnce: true}},
		Action: func(_ context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return cli.Exit(i18n.InvalidInvocation, 2)
			}
			config, err := commandConfig(command)
			if err != nil {
				if _, ok := err.(*runtimeError); ok {
					_, _ = fmt.Fprintln(stderr, i18n.DoctorSetupHint)
				}
				return err
			}
			// 地址可能由用户覆盖；即使 Key 意外出现在地址中，也不允许被诊断输出回显。
			gateway := strings.ReplaceAll(config.GatewayURL, config.Key, i18n.Redacted)
			if command.Bool("json") {
				return json.NewEncoder(stdout).Encode(struct {
					GatewayURL     string `json:"gateway_url"`
					KeyConfigured  bool   `json:"key_configured"`
					NetworkChecked bool   `json:"network_checked"`
				}{GatewayURL: gateway, KeyConfigured: true})
			}
			_, err = fmt.Fprintf(stdout, i18n.DoctorReady, gateway)
			return err
		},
	}}
}

// commandConfig 保留显式配置与地址覆盖的校验，禁止无效参数触发默认地址回退。
func commandConfig(command *cli.Command) (runtimeConfig, error) {
	for _, name := range []string{"gateway-url", "core-url"} {
		if command.IsSet(name) && !validOrigin(command.String(name), true) {
			return runtimeConfig{}, cli.Exit(i18n.InvalidInvocation, 2)
		}
	}
	for _, name := range []string{"config", "profile"} {
		if command.IsSet(name) && command.String(name) == "" {
			return runtimeConfig{}, cli.Exit(i18n.InvalidInvocation, 2)
		}
	}
	config, err := loadConfig(command.String("config"), command.String("profile"), command.String("gateway-url"), command.String("core-url"))
	if err != nil {
		return runtimeConfig{}, &runtimeError{Code: i18n.ConfigurationError}
	}
	return config, nil
}
