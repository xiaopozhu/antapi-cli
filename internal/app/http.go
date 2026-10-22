package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/xiaopozhu/antapi-cli/internal/i18n"
)

const maxResponseBytes = 1 << 20

var safeIdentity = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
var safeAtoms = regexp.MustCompile(`^[0-9]{1,40}$`)

type responseMetadata struct {
	HTTPStatus        int    `json:"http_status"`
	RequestID         string `json:"request_id,omitempty"`
	ReceiptID         string `json:"receipt_id,omitempty"`
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

func gatewayGET(ctx context.Context, config runtimeConfig, path string) (commandResult, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(config.GatewayURL, "/")+path, nil)
	if err != nil {
		return commandResult{}, &runtimeError{Code: i18n.NetworkError}
	}
	request.Header.Set("Authorization", "Bearer "+config.Key)
	request.Header.Set("Accept", "application/json")
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
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: config.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return commandResult{}, &runtimeError{Code: i18n.NetworkError}
	}
	defer response.Body.Close()
	metadata := responseMetadata{HTTPStatus: response.StatusCode,
		RequestID:         filteredHeader(response.Header.Get("X-Request-ID"), config.Key, safeIdentity),
		ReceiptID:         filteredHeader(response.Header.Get("X-AntAPI-Receipt-ID"), config.Key, safeIdentity),
		CreditsDebitAtoms: filteredHeader(response.Header.Get("X-AntAPI-Credits-Debit-Atoms"), config.Key, safeAtoms)}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes+1))
	if err != nil || len(body) > maxResponseBytes {
		return commandResult{}, &runtimeError{Code: i18n.ResponseError, Metadata: metadata}
	}
	var envelope struct {
		Code *uint32 `json:"code"`
		Data any     `json:"data"`
	}
	decoder := json.NewDecoder(strings.NewReader(string(body)))
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
	return commandResult{Code: *envelope.Code, Data: normalizeData(envelope.Data, "", config.Key), Metadata: metadata}, nil
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
