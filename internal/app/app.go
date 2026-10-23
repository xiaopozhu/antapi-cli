// Package app 定义命令入口；帮助与版本查询不初始化认证或网络依赖。
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/urfave/cli/v3"

	"github.com/xiaopozhu/antapi-cli/internal/i18n"
)

// BuildInfo 保存编译时注入的信息，不包含凭据或运行配置。
type BuildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
}

// Run 执行命令并返回退出码；参数错误不回显用户输入。
func Run(ctx context.Context, args []string, stdout, stderr io.Writer, info BuildInfo) int {
	usageError := func(context.Context, *cli.Command, error, bool) error {
		return cli.Exit(i18n.InvalidInvocation, 2)
	}
	command := &cli.Command{
		Name: "antapi", Usage: i18n.AppUsage, Description: i18n.AppDescription, Version: info.Version,
		Writer: stdout, ErrWriter: io.Discard, HideHelpCommand: true,
		OnUsageError: usageError,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "config", Usage: i18n.ConfigUsage, OnlyOnce: true},
			&cli.StringFlag{Name: "profile", Usage: i18n.ProfileUsage, OnlyOnce: true},
			&cli.StringFlag{Name: "gateway-url", Usage: i18n.GatewayUsage, OnlyOnce: true},
			&cli.StringFlag{Name: "core-url", Usage: i18n.CoreUsage, OnlyOnce: true},
		},
		ExitErrHandler: func(context.Context, *cli.Command, error) {},
		Action: func(ctx context.Context, command *cli.Command) error {
			if command.Args().Len() != 0 {
				return cli.Exit(i18n.InvalidInvocation, 2)
			}
			return cli.ShowAppHelp(command)
		},
		Commands: []*cli.Command{whoisCommand(stdout, usageError), {
			Name: "version", Usage: i18n.VersionUsage, OnUsageError: usageError,
			Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: i18n.JSONUsage, OnlyOnce: true}},
			Action: func(_ context.Context, command *cli.Command) error {
				if command.Args().Len() != 0 {
					return cli.Exit(i18n.InvalidInvocation, 2)
				}
				if command.Bool("json") {
					return json.NewEncoder(stdout).Encode(info)
				}
				_, err := fmt.Fprintf(stdout, i18n.VersionFormat, info.Version, info.Commit, info.BuiltAt)
				return err
			},
		}},
	}
	command.Commands = append(command.Commands, onboardingCommands(stdout, stderr, usageError)...)
	if err := command.Run(ctx, args); err != nil {
		var exitCoder cli.ExitCoder
		if errors.As(err, &exitCoder) && exitCoder.ExitCode() == 2 {
			_, _ = fmt.Fprintln(stderr, i18n.InvalidInvocation)
			return 2
		}
		writeRuntimeError(stderr, err)
		return 1
	}
	return 0
}

func whoisCommand(stdout io.Writer, usageError cli.OnUsageErrorFunc) *cli.Command {
	invalid := func() error { return cli.Exit(i18n.InvalidInvocation, 2) }
	action := func(lookup bool) cli.ActionFunc {
		return func(ctx context.Context, command *cli.Command) error {
			count := command.Args().Len()
			if (lookup && (count != 1 || !validDomain(command.Args().First()))) || (!lookup && count != 0) {
				return invalid()
			}
			config, err := commandConfig(command)
			if err != nil {
				return err
			}
			path := "/whois/v1/tlds"
			if lookup {
				path = lookupPath(command.Args().First())
			}
			result, err := gatewayGET(ctx, config, path)
			if err != nil {
				return err
			}
			// 两种输出模式都保留完整结构；--json 是供自动化调用声明的稳定协议。
			return json.NewEncoder(stdout).Encode(result)
		}
	}
	return &cli.Command{Name: "whois", Usage: i18n.WhoisUsage, OnUsageError: usageError,
		Action: func(ctx context.Context, c *cli.Command) error {
			if c.Args().Len() != 0 {
				return invalid()
			}
			return cli.ShowSubcommandHelp(c)
		},
		Commands: []*cli.Command{
			{Name: "lookup", Usage: i18n.LookupUsage, ArgsUsage: i18n.DomainArgument, OnUsageError: usageError, Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: i18n.JSONUsage, OnlyOnce: true}}, Action: action(true)},
			{Name: "tlds", Usage: i18n.TLDUsage, OnUsageError: usageError, Flags: []cli.Flag{&cli.BoolFlag{Name: "json", Usage: i18n.JSONUsage, OnlyOnce: true}}, Action: action(false)},
		}}
}
