package app

import (
	"context"
	"encoding/json"
	"io"
	"net/url"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/urfave/cli/v3"

	"github.com/xiaopozhu/antapi-cli/internal/i18n"
)

func validIdempotencyKey(value string) bool {
	return value == "" || safeIdentity.MatchString(value)
}

func validRegularFile(path string, maxBytes int64) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0 && info.Size() <= maxBytes
}

func validPublicHTTPURL(raw string) bool {
	if len(raw) == 0 || len(raw) > 8192 || !utf8.ValidString(raw) || strings.ContainsAny(raw, " \t\r\n") {
		return false
	}
	parsed, err := url.Parse(raw)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && parsed.Hostname() != ""
}

func withDefaultTimeout(config runtimeConfig, fallback time.Duration) runtimeConfig {
	if !config.TimeoutExplicit {
		config.Timeout = fallback
	}
	return config
}

func webCommand(stdout io.Writer, usageError cli.OnUsageErrorFunc) *cli.Command {
	invalid := func() error { return cli.Exit(i18n.InvalidInvocation, 2) }
	return &cli.Command{
		Name: "web", Usage: i18n.WebUsage, OnUsageError: usageError,
		Action: func(ctx context.Context, c *cli.Command) error {
			if c.Args().Len() != 0 {
				return invalid()
			}
			return cli.ShowSubcommandHelp(c)
		},
		Commands: []*cli.Command{
			{
				Name: "search", Usage: i18n.WebSearchUsage, ArgsUsage: i18n.QueryArgument, OnUsageError: usageError,
				Flags: []cli.Flag{
					&cli.IntFlag{Name: "count", Usage: i18n.CountUsage, OnlyOnce: true},
					&cli.StringFlag{Name: "localization", Usage: i18n.LocalizationUsage, OnlyOnce: true},
					&cli.BoolFlag{Name: "need-content", Usage: i18n.NeedContentUsage, OnlyOnce: true},
					&cli.StringFlag{Name: "ori-question", Usage: i18n.OriQuestionUsage, OnlyOnce: true},
					&cli.StringFlag{Name: "idempotency-key", Usage: i18n.IdempotencyUsage, OnlyOnce: true},
					&cli.BoolFlag{Name: "json", Usage: i18n.JSONUsage, OnlyOnce: true},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() != 1 {
						return invalid()
					}
					query := strings.TrimSpace(c.Args().First())
					if len(query) == 0 || len(query) > 128 || !utf8.ValidString(query) {
						return invalid()
					}
					count := c.Int("count")
					if c.IsSet("count") && (count < 1 || count > 50) {
						return invalid()
					}
					oriQuestion := strings.TrimSpace(c.String("ori-question"))
					if c.IsSet("ori-question") && (len(oriQuestion) == 0 || len(oriQuestion) > 128 || !utf8.ValidString(oriQuestion)) {
						return invalid()
					}
					idempotencyKey := c.String("idempotency-key")
					if !validIdempotencyKey(idempotencyKey) {
						return invalid()
					}
					config, err := commandConfig(c)
					if err != nil {
						return err
					}
					payload := map[string]any{"query": query}
					if c.IsSet("count") {
						payload["count"] = count
					}
					if loc := strings.TrimSpace(c.String("localization")); loc != "" {
						payload["localization"] = loc
					}
					if c.IsSet("need-content") {
						payload["need_content"] = c.Bool("need-content")
					}
					if oriQuestion != "" {
						payload["ori_question"] = oriQuestion
					}
					body, err := json.Marshal(payload)
					if err != nil {
						return invalid()
					}
					result, err := gatewayPOSTJSON(ctx, withDefaultTimeout(config, defaultToolTimeout), "/web/v1/search", body, idempotencyKey, maxToolResponseBytes)
					if err != nil {
						return err
					}
					return json.NewEncoder(stdout).Encode(result)
				},
			},
			{
				Name: "fetch", Usage: i18n.WebFetchUsage, ArgsUsage: i18n.URLArgument, OnUsageError: usageError,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "idempotency-key", Usage: i18n.IdempotencyUsage, OnlyOnce: true},
					&cli.BoolFlag{Name: "json", Usage: i18n.JSONUsage, OnlyOnce: true},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() != 1 {
						return invalid()
					}
					targetURL := strings.TrimSpace(c.Args().First())
					idempotencyKey := c.String("idempotency-key")
					if !validPublicHTTPURL(targetURL) || !validIdempotencyKey(idempotencyKey) {
						return invalid()
					}
					config, err := commandConfig(c)
					if err != nil {
						return err
					}
					body, err := json.Marshal(map[string]string{"url": targetURL})
					if err != nil {
						return invalid()
					}
					result, err := gatewayPOSTJSON(ctx, withDefaultTimeout(config, defaultToolTimeout), "/web/v1/fetch", body, idempotencyKey, maxToolResponseBytes)
					if err != nil {
						return err
					}
					return json.NewEncoder(stdout).Encode(result)
				},
			},
		},
	}
}

func ocrCommand(stdout io.Writer, usageError cli.OnUsageErrorFunc) *cli.Command {
	invalid := func() error { return cli.Exit(i18n.InvalidInvocation, 2) }
	return &cli.Command{
		Name: "ocr", Usage: i18n.OCRUsage, OnUsageError: usageError,
		Action: func(ctx context.Context, c *cli.Command) error {
			if c.Args().Len() != 0 {
				return invalid()
			}
			return cli.ShowSubcommandHelp(c)
		},
		Commands: []*cli.Command{
			{
				Name: "recognize", Usage: i18n.OCRRecognizeUsage, ArgsUsage: i18n.FileArgument, OnUsageError: usageError,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "purpose", Value: "text_recognition", Usage: i18n.PurposeUsage, OnlyOnce: true},
					&cli.StringFlag{Name: "idempotency-key", Usage: i18n.IdempotencyUsage, OnlyOnce: true},
					&cli.BoolFlag{Name: "json", Usage: i18n.JSONUsage, OnlyOnce: true},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() != 1 {
						return invalid()
					}
					purpose := c.String("purpose")
					idempotencyKey := c.String("idempotency-key")
					filePath := c.Args().First()
					if (purpose != "text_recognition" && purpose != "rich_layout_recognition") || !validIdempotencyKey(idempotencyKey) || !validRegularFile(filePath, maxOCRFileBytes) {
						return invalid()
					}
					config, err := commandConfig(c)
					if err != nil {
						return err
					}
					result, err := gatewayPOSTMultipart(ctx, withDefaultTimeout(config, defaultLongAPITimeout), "/ocr/v1/recognize", map[string]string{"purpose": purpose}, "file", filePath, maxOCRFileBytes, idempotencyKey, maxToolResponseBytes)
					if err != nil {
						return err
					}
					return json.NewEncoder(stdout).Encode(result)
				},
			},
		},
	}
}

func asrCommand(stdout io.Writer, usageError cli.OnUsageErrorFunc) *cli.Command {
	invalid := func() error { return cli.Exit(i18n.InvalidInvocation, 2) }
	return &cli.Command{
		Name: "asr", Usage: i18n.ASRUsage, OnUsageError: usageError,
		Action: func(ctx context.Context, c *cli.Command) error {
			if c.Args().Len() != 0 {
				return invalid()
			}
			return cli.ShowSubcommandHelp(c)
		},
		Commands: []*cli.Command{
			{
				Name: "transcribe", Usage: i18n.ASRTranscribeUsage, ArgsUsage: i18n.FileArgument, OnUsageError: usageError,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "idempotency-key", Usage: i18n.IdempotencyUsage, OnlyOnce: true},
					&cli.BoolFlag{Name: "json", Usage: i18n.JSONUsage, OnlyOnce: true},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() != 1 {
						return invalid()
					}
					idempotencyKey := c.String("idempotency-key")
					filePath := c.Args().First()
					if !validIdempotencyKey(idempotencyKey) || !validRegularFile(filePath, maxASRFileBytes) {
						return invalid()
					}
					config, err := commandConfig(c)
					if err != nil {
						return err
					}
					result, err := gatewayPOSTMultipart(ctx, withDefaultTimeout(config, defaultLongAPITimeout), "/asr/v1/transcribe", nil, "file", filePath, maxASRFileBytes, idempotencyKey, maxToolResponseBytes)
					if err != nil {
						return err
					}
					return json.NewEncoder(stdout).Encode(result)
				},
			},
		},
	}
}

func ttsCommand(stdout io.Writer, usageError cli.OnUsageErrorFunc) *cli.Command {
	invalid := func() error { return cli.Exit(i18n.InvalidInvocation, 2) }
	return &cli.Command{
		Name: "tts", Usage: i18n.TTSUsage, OnUsageError: usageError,
		Action: func(ctx context.Context, c *cli.Command) error {
			if c.Args().Len() != 0 {
				return invalid()
			}
			return cli.ShowSubcommandHelp(c)
		},
		Commands: []*cli.Command{
			{
				Name: "synthesize", Usage: i18n.TTSSynthesizeUsage, OnUsageError: usageError,
				Flags: []cli.Flag{
					&cli.StringFlag{Name: "text", Usage: i18n.TextUsage, OnlyOnce: true},
					&cli.StringFlag{Name: "text-file", Usage: i18n.TextFileUsage, OnlyOnce: true},
					&cli.StringFlag{Name: "output", Usage: i18n.OutputUsage, OnlyOnce: true},
					&cli.StringFlag{Name: "voice", Value: "stella", Usage: i18n.VoiceUsage, OnlyOnce: true},
					&cli.BoolFlag{Name: "filter-markdown", Value: true, Usage: i18n.FilterMarkdownUsage, OnlyOnce: true},
					&cli.StringFlag{Name: "idempotency-key", Usage: i18n.IdempotencyUsage, OnlyOnce: true},
					&cli.BoolFlag{Name: "json", Usage: i18n.JSONUsage, OnlyOnce: true},
				},
				Action: func(ctx context.Context, c *cli.Command) error {
					if c.Args().Len() != 0 {
						return invalid()
					}
					hasText := c.IsSet("text")
					hasTextFile := c.IsSet("text-file")
					if hasText == hasTextFile {
						return invalid()
					}
					outputPath := strings.TrimSpace(c.String("output"))
					voice := c.String("voice")
					idempotencyKey := c.String("idempotency-key")
					if outputPath == "" || (voice != "stella" && voice != "tide") || !validIdempotencyKey(idempotencyKey) {
						return invalid()
					}
					var text string
					if hasText {
						text = c.String("text")
					} else {
						textPath := c.String("text-file")
						if !validRegularFile(textPath, maxTTSBodyBytes) {
							return invalid()
						}
						raw, err := os.ReadFile(textPath)
						if err != nil {
							return invalid()
						}
						text = string(raw)
					}
					if strings.TrimSpace(text) == "" || !utf8.ValidString(text) {
						return invalid()
					}
					payload := map[string]any{
						"text":            text,
						"voice_id":        voice,
						"filter_markdown": c.Bool("filter-markdown"),
					}
					body, err := json.Marshal(payload)
					if err != nil || len(body) > maxTTSBodyBytes {
						return invalid()
					}
					config, err := commandConfig(c)
					if err != nil {
						return err
					}
					result, err := gatewayPOSTJSONToFile(ctx, withDefaultTimeout(config, defaultLongAPITimeout), "/tts/v1/synthesize", body, idempotencyKey, outputPath, maxTTSAudioBytes)
					if err != nil {
						return err
					}
					return json.NewEncoder(stdout).Encode(result)
				},
			},
		},
	}
}
