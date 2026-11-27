package app

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/xiaopozhu/antapi-cli/internal/i18n"
)

const (
	maxResponseBytes      = 1 << 20
	maxToolResponseBytes  = 4 << 20
	maxTTSAudioBytes      = 86400044
	maxOCRFileBytes       = 100 << 20
	maxASRFileBytes       = 8 << 20
	maxTTSBodyBytes       = 64 << 10
	defaultToolTimeout    = 40 * time.Second
	defaultLongAPITimeout = 240 * time.Second
)

var safeIdentity = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var safeAtoms = regexp.MustCompile(`^[0-9]{1,40}$`)

type responseMetadata struct {
	HTTPStatus        int    `json:"http_status"`
	RequestID         string `json:"request_id,omitempty"`
	ReceiptID         string `json:"receipt_id,omitempty"`
	DebitAtoms        string `json:"debit_atoms,omitempty"`
	CreditsDebitAtoms string `json:"credits_debit_atoms,omitempty"`
}
type commandResult struct {
	Code     uint32           `json:"code"`
	Data     any              `json:"data"`
	Metadata responseMetadata `json:"metadata"`
}
type runtimeError struct {
	Code     string
	API      *uint32
	Metadata responseMetadata
}

func (e *runtimeError) Error() string { return e.Code }

func validDomain(value string) bool {
	if len(value) == 0 || len(value) > 1024 || !utf8.ValidString(value) || strings.ContainsAny(value, "/\\:%?#@") {
		return false
	}
	// 只检查路径安全与裸域名结构；完整 IDNA 映射及注册域校验由公开 API 负责。
	value = strings.NewReplacer("。", ".", "．", ".", "｡", ".").Replace(value)
	value = strings.TrimSuffix(value, ".")
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if len(label) == 0 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if unicode.IsControl(r) || unicode.IsSpace(r) || (r < 128 && !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-')) {
				return false
			}
		}
	}
	return true
}

func filteredHeader(value, key string, pattern *regexp.Regexp) string {
	if strings.Contains(value, key) || !pattern.MatchString(value) {
		return ""
	}
	return value
}

func newGatewayClient(timeout time.Duration) (*http.Client, func()) {
	// 每次只执行一个请求；不重定向，不复用连接，避免 net/http 的透明请求重放。
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	// HTTP/2 会对 REFUSED_STREAM 等错误透明重发；付费调用固定使用 HTTP/1。
	transport.ForceAttemptHTTP2 = false
	transport.Protocols = new(http.Protocols)
	transport.Protocols.SetHTTP1(true)
	transport.Protocols.SetHTTP2(false)
	if transport.TLSClientConfig == nil {
		transport.TLSClientConfig = &tls.Config{}
	} else {
		transport.TLSClientConfig = transport.TLSClientConfig.Clone()
	}
	// 同时覆盖继承的 ALPN，防止协议被禁用后仍协商 h2。
	transport.TLSClientConfig.NextProtos = []string{"http/1.1"}
	client := &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return client, transport.CloseIdleConnections
}

func extractMetadata(response *http.Response, key string) responseMetadata {
	return responseMetadata{
		HTTPStatus:        response.StatusCode,
		RequestID:         filteredHeader(response.Header.Get("X-Request-ID"), key, safeIdentity),
		ReceiptID:         filteredHeader(response.Header.Get("X-AntAPI-Receipt-ID"), key, safeIdentity),
		DebitAtoms:        filteredHeader(response.Header.Get("X-AntAPI-Debit-Atoms"), key, safeAtoms),
		CreditsDebitAtoms: filteredHeader(response.Header.Get("X-AntAPI-Credits-Debit-Atoms"), key, safeAtoms),
	}
}

func decodeGatewayJSON(response *http.Response, key string, limit int64) (commandResult, error) {
	metadata := extractMetadata(response, key)
	body, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(body)) > limit {
		return commandResult{}, &runtimeError{Code: i18n.ResponseError, Metadata: metadata}
	}
	var envelope struct {
		Code *uint32 `json:"code"`
		Data any     `json:"data"`
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if decoder.Decode(&envelope) != nil || envelope.Code == nil {
		return commandResult{}, &runtimeError{Code: i18n.ResponseError, Metadata: metadata}
	}
	var trailing any
	if decoder.Decode(&trailing) != io.EOF {
		return commandResult{}, &runtimeError{Code: i18n.ResponseError, Metadata: metadata}
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || *envelope.Code != 0 {
		return commandResult{}, &runtimeError{Code: i18n.PlatformError, API: envelope.Code, Metadata: metadata}
	}
	return commandResult{Code: *envelope.Code, Data: normalizeData(envelope.Data, "", key), Metadata: metadata}, nil
}

func gatewayGET(ctx context.Context, config runtimeConfig, path string) (commandResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(config.GatewayURL, "/")+path, nil)
	if err != nil {
		return commandResult{}, &runtimeError{Code: i18n.NetworkError}
	}
	request.Header.Set("Authorization", "Bearer "+config.Key)
	request.Header.Set("Accept", "application/json")
	client, cleanup := newGatewayClient(config.Timeout)
	defer cleanup()
	response, err := client.Do(request)
	if err != nil {
		return commandResult{}, &runtimeError{Code: i18n.NetworkError}
	}
	defer response.Body.Close()
	return decodeGatewayJSON(response, config.Key, maxResponseBytes)
}

func gatewayPOSTJSON(ctx context.Context, config runtimeConfig, path string, body []byte, idempotencyKey string, limit int64) (commandResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(config.GatewayURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return commandResult{}, &runtimeError{Code: i18n.NetworkError}
	}
	request.Header.Set("Authorization", "Bearer "+config.Key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	client, cleanup := newGatewayClient(config.Timeout)
	defer cleanup()
	response, err := client.Do(request)
	if err != nil {
		return commandResult{}, &runtimeError{Code: i18n.NetworkError}
	}
	defer response.Body.Close()
	return decodeGatewayJSON(response, config.Key, limit)
}

func gatewayPOSTMultipart(ctx context.Context, config runtimeConfig, path string, fields map[string]string, fileField, filePath string, maxFileBytes int64, idempotencyKey string, limit int64) (commandResult, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return commandResult{}, &runtimeError{Code: i18n.ConfigurationError}
	}
	defer file.Close()

	// 流式构造 multipart，避免把百兆文件整份读入内存。
	pr, pw := io.Pipe()
	writer := multipart.NewWriter(pw)
	go func() {
		var writeErr error
		defer func() {
			_ = writer.Close()
			_ = pw.CloseWithError(writeErr)
		}()
		for k, v := range fields {
			if writeErr = writer.WriteField(k, v); writeErr != nil {
				return
			}
		}
		part, err := writer.CreateFormFile(fileField, filepath.Base(filePath))
		if err != nil {
			writeErr = err
			return
		}
		written, err := io.Copy(part, io.LimitReader(file, maxFileBytes+1))
		if err != nil {
			writeErr = err
			return
		}
		if written > maxFileBytes {
			writeErr = io.ErrShortWrite
		}
	}()

	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(config.GatewayURL, "/")+path, pr)
	if err != nil {
		_ = pr.Close()
		return commandResult{}, &runtimeError{Code: i18n.NetworkError}
	}
	request.Header.Set("Authorization", "Bearer "+config.Key)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	request.Header.Set("Accept", "application/json")
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	client, cleanup := newGatewayClient(config.Timeout)
	defer cleanup()
	response, err := client.Do(request)
	if err != nil {
		_ = pr.Close()
		return commandResult{}, &runtimeError{Code: i18n.NetworkError}
	}
	defer response.Body.Close()
	return decodeGatewayJSON(response, config.Key, limit)
}

func gatewayPOSTJSONToFile(ctx context.Context, config runtimeConfig, path string, body []byte, idempotencyKey, outputPath string, limit int64) (commandResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimSuffix(config.GatewayURL, "/")+path, bytes.NewReader(body))
	if err != nil {
		return commandResult{}, &runtimeError{Code: i18n.NetworkError}
	}
	request.Header.Set("Authorization", "Bearer "+config.Key)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "audio/wav, application/json")
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}
	client, cleanup := newGatewayClient(config.Timeout)
	defer cleanup()
	response, err := client.Do(request)
	if err != nil {
		return commandResult{}, &runtimeError{Code: i18n.NetworkError}
	}
	defer response.Body.Close()

	metadata := extractMetadata(response, config.Key)
	contentType := strings.ToLower(strings.TrimSpace(strings.Split(response.Header.Get("Content-Type"), ";")[0]))
	if response.StatusCode < 200 || response.StatusCode >= 300 || contentType == "application/json" {
		return decodeGatewayJSON(response, config.Key, maxToolResponseBytes)
	}

	dir := filepath.Dir(outputPath)
	tmp, err := os.CreateTemp(dir, ".antapi-tts-*.tmp")
	if err != nil {
		return commandResult{}, &runtimeError{Code: i18n.ConfigurationError, Metadata: metadata}
	}
	tmpPath := tmp.Name()
	success := false
	defer func() {
		if !success {
			_ = tmp.Close()
			_ = os.Remove(tmpPath)
		}
	}()

	written, err := io.Copy(tmp, io.LimitReader(response.Body, limit+1))
	if err != nil || written == 0 || written > limit {
		return commandResult{}, &runtimeError{Code: i18n.ResponseError, Metadata: metadata}
	}
	if err = tmp.Close(); err != nil {
		return commandResult{}, &runtimeError{Code: i18n.ConfigurationError, Metadata: metadata}
	}
	if err = os.Rename(tmpPath, outputPath); err != nil {
		return commandResult{}, &runtimeError{Code: i18n.ConfigurationError, Metadata: metadata}
	}
	success = true
	return commandResult{
		Code: 0,
		Data: map[string]any{
			"output":       outputPath,
			"bytes":        written,
			"content_type": contentType,
		},
		Metadata: metadata,
	}, nil
}

func normalizeData(value any, name, key string) any {
	switch typed := value.(type) {
	case map[string]any:
		normalized := make(map[string]any, len(typed))
		for k, v := range typed {
			normalized[strings.ReplaceAll(k, key, i18n.Redacted)] = normalizeData(v, k, key)
		}
		return normalized
	case []any:
		for i, v := range typed {
			typed[i] = normalizeData(v, name, key)
		}
		return typed
	case json.Number:
		if strings.Contains(typed.String(), key) {
			return i18n.Redacted
		}
		if name == "id" || name == "ids" || strings.HasSuffix(name, "_id") || strings.HasSuffix(name, "_ids") || strings.HasSuffix(name, "_atoms") {
			return typed.String()
		}
		return typed
	case string:
		return strings.ReplaceAll(typed, key, i18n.Redacted)
	default:
		return value
	}
}

func lookupPath(domain string) string { return "/whois/v1/domains/" + url.PathEscape(domain) }

func writeRuntimeError(writer io.Writer, err error) {
	if known, ok := err.(*runtimeError); ok {
		_, _ = fmt.Fprint(writer, known.Code)
		if known.API != nil {
			_, _ = fmt.Fprintf(writer, i18n.APICodeFormat, *known.API)
		}
		if known.Metadata.RequestID != "" {
			_, _ = fmt.Fprintf(writer, i18n.RequestIDFormat, known.Metadata.RequestID)
		}
		_, _ = fmt.Fprintln(writer)
		return
	}
	_, _ = fmt.Fprintln(writer, i18n.ExecutionFailed)
}
