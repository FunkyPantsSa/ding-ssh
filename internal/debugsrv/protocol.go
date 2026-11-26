package debugsrv

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	mcpSessionTTL  = 30 * time.Minute
	mcpMaxSessions = 128
)

type protocolContextKey struct{}
type sessionContextKey struct{}

func protocolVersion(ctx context.Context) string {
	if v, ok := ctx.Value(protocolContextKey{}).(string); ok {
		return v
	}
	return "2025-06-18"
}

func protocolSupported(v string) bool {
	return v == "2024-11-05" || v == "2025-03-26" || v == "2025-06-18"
}

// transportSafety rejects DNS rebinding/cross-origin browser requests before
// authentication and bounds concurrent work. The bearer credential is accepted
// only in a header; URL credentials may enter browser and proxy histories.
func (s *Server) transportSafety(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		if !s.requestHostAllowed(r) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "untrusted request host"})
			return
		}
		if raw := r.Header.Get("Origin"); raw != "" {
			u, err := url.Parse(raw)
			if err != nil || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || (u.Scheme != "http" && u.Scheme != "https") || !strings.EqualFold(u.Host, r.Host) {
				writeJSON(w, http.StatusForbidden, map[string]any{"error": "untrusted request origin"})
				return
			}
		}
		slots := s.httpSlots
		if isStreamRequest(r) {
			slots = s.streamSlots
		}
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many concurrent requests"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestHostAllowed(r *http.Request) bool {
	host, port, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	host = strings.Trim(host, "[]")
	if s.ln != nil {
		_, wantPort, _ := net.SplitHostPort(s.ln.Addr().String())
		if port != wantPort {
			return false
		}
	}
	bindHost, _, _ := net.SplitHostPort(s.opts.Addr)
	bindIP := net.ParseIP(bindHost)
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	if ip.IsLoopback() {
		return true
	}
	if bindIP != nil && !bindIP.IsUnspecified() {
		return ip.Equal(bindIP)
	}
	if addrs, err := net.InterfaceAddrs(); err == nil {
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
				return true
			}
		}
	}
	return false
}

func validateRPC(req rpcRequest) error {
	if req.JSONRPC != "2.0" || strings.TrimSpace(req.Method) == "" {
		return fmt.Errorf("jsonrpc 必须为 2.0 且 method 不能为空")
	}
	if len(req.ID) > 0 {
		var id any
		if err := json.Unmarshal(req.ID, &id); err != nil {
			return fmt.Errorf("id 无效")
		}
		switch v := id.(type) {
		case string:
			if v == "" {
				return fmt.Errorf("id 不能为空")
			}
		case float64:
			if math.IsNaN(v) || math.IsInf(v, 0) || math.Trunc(v) != v || math.Abs(v) > 9007199254740991 {
				return fmt.Errorf("id 必须为整数或字符串")
			}
		default:
			return fmt.Errorf("id 必须为整数或字符串")
		}
	}
	params := strings.TrimSpace(string(req.Params))
	if params != "" && params != "null" && params[0] != '{' {
		return fmt.Errorf("params 必须是对象")
	}
	return nil
}

func rpcRequestIDKey(raw json.RawMessage) string {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return string(raw)
	}
	if number, ok := value.(float64); ok && number == 0 {
		return "0"
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return string(raw)
	}
	return string(encoded)
}

// validateToolSchema implements the subset emitted by this server, recursively.
// Error messages contain field names only, never the supplied values.
func validateToolSchema(schema map[string]any, value any, field string) error {
	typ, _ := schema["type"].(string)
	bad := func() error { return fmt.Errorf("参数 %s 必须为 %s", field, typ) }
	var values []any
	switch enumeration := schema["enum"].(type) {
	case []any:
		values = enumeration
	case []string:
		values = make([]any, len(enumeration))
		for i, v := range enumeration {
			values[i] = v
		}
	}
	if values != nil {
		encoded, _ := json.Marshal(value)
		matched := false
		for _, candidate := range values {
			other, _ := json.Marshal(candidate)
			if string(encoded) == string(other) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("参数 %s 不在允许值中", field)
		}
	}
	switch typ {
	case "object":
		m, ok := value.(map[string]any)
		if !ok {
			return bad()
		}
		props, _ := schema["properties"].(map[string]any)
		var required []string
		switch r := schema["required"].(type) {
		case []string:
			required = r
		case []any:
			for _, v := range r {
				if k, ok := v.(string); ok {
					required = append(required, k)
				}
			}
		}
		for _, k := range required {
			if _, ok := m[k]; !ok {
				return fmt.Errorf("缺少参数 %s.%s", field, k)
			}
		}
		for k, v := range m {
			// token/confirm are transport authorization fields, accepted consistently
			// even for older tool schemas that did not declare the aliases.
			if field == "arguments" && (k == "token" || k == "confirm") {
				if _, ok := v.(string); !ok {
					return fmt.Errorf("参数 %s 必须为 string", k)
				}
				continue
			}
			p, exists := props[k]
			if !exists {
				if closed, ok := schema["additionalProperties"].(bool); ok && !closed {
					return fmt.Errorf("未知参数 %s.%s", field, k)
				}
				if child, ok := schema["additionalProperties"].(map[string]any); ok {
					if err := validateToolSchema(child, v, field+"."+k); err != nil {
						return err
					}
				}
				continue
			}
			if child, ok := p.(map[string]any); ok {
				if err := validateToolSchema(child, v, field+"."+k); err != nil {
					return err
				}
			}
		}
	case "array":
		arr, ok := value.([]any)
		if !ok {
			return bad()
		}
		if min, ok := schemaNumber(schema["minItems"]); ok && float64(len(arr)) < min {
			return fmt.Errorf("参数 %s 的条目不足", field)
		}
		if max, ok := schemaNumber(schema["maxItems"]); ok && float64(len(arr)) > max {
			return fmt.Errorf("参数 %s 的条目过多", field)
		}
		if child, ok := schema["items"].(map[string]any); ok {
			for _, v := range arr {
				if err := validateToolSchema(child, v, field+"[]"); err != nil {
					return err
				}
			}
		}
	case "string":
		str, ok := value.(string)
		if !ok {
			return bad()
		}
		if min, ok := schemaNumber(schema["minLength"]); ok && float64(utf8.RuneCountInString(str)) < min {
			return fmt.Errorf("参数 %s 过短", field)
		}
		if max, ok := schemaNumber(schema["maxLength"]); ok && float64(utf8.RuneCountInString(str)) > max {
			return fmt.Errorf("参数 %s 过长", field)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return bad()
		}
	case "number", "integer":
		var f float64
		switch n := value.(type) {
		case float64:
			f = n
		case int:
			f = float64(n)
		case int64:
			f = float64(n)
		case json.Number:
			var err error
			f, err = n.Float64()
			if err != nil {
				return bad()
			}
		default:
			return bad()
		}
		if math.IsNaN(f) || math.IsInf(f, 0) || (typ == "integer" && (math.Trunc(f) != f || math.Abs(f) > 9007199254740991)) {
			return bad()
		}
		if n, ok := schemaNumber(schema["minimum"]); ok && f < n {
			return fmt.Errorf("参数 %s 小于允许范围", field)
		}
		if n, ok := schemaNumber(schema["maximum"]); ok && f > n {
			return fmt.Errorf("参数 %s 超出允许范围", field)
		}
	}
	return nil
}

func schemaNumber(value any) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case float64:
		return v, true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	}
	return 0, false
}

func (s *Server) eventPage(r *http.Request) map[string]any {
	since, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return s.eventPageContext(r.Context(), since, limit, strings.Split(r.URL.Query().Get("topics"), ",")...)
}

func (s *Server) eventPageContext(ctx context.Context, since uint64, limit int, topics ...string) map[string]any {
	page := s.opts.Hub.Since(since, limit, topics...)
	if events, ok := page["events"].([]Event); ok {
		filtered := events[:0]
		for _, ev := range events {
			if s.authorizeEvent(ctx, ev) {
				filtered = append(filtered, ev)
			}
		}
		page["events"] = filtered
		page["count"] = len(filtered)
	}
	return page
}

func validResourceURI(uri string) bool {
	switch uri {
	case "app://state", "app://debug/info", "app://servers", "app://logs", "app://logs/tail", "ui://layout", "ui://view", "ui://console", TransfersResourceURI:
		return true
	}
	for _, shape := range [][2]string{{"terminal://", "/buffer"}, {"sftp://", "/cwd"}} {
		if !strings.HasPrefix(uri, shape[0]) || !strings.HasSuffix(uri, shape[1]) {
			continue
		}
		id := strings.TrimSuffix(strings.TrimPrefix(uri, shape[0]), shape[1])
		return id != "" && !strings.ContainsAny(id, "/?#\\ \t\r\n")
	}
	return false
}

func toolErrorMeta(err error) map[string]any {
	var operation *OperationError
	if errors.As(err, &operation) {
		meta := map[string]any{"errorCode": operation.Code, "retryable": operation.Retryable, "outcomeKnown": !operation.OutcomeUnknown}
		if len(operation.MissingCapabilities) > 0 {
			meta["missingCapabilities"] = operation.MissingCapabilities
		}
		if len(operation.NextSteps) > 0 {
			meta["nextSteps"] = operation.NextSteps
		}
		if operation.RetryAfterMs > 0 {
			meta["retryAfterMs"] = operation.RetryAfterMs
		}
		return meta
	}
	code := "OPERATION_FAILED"
	retryable := false
	switch {
	case errors.Is(err, context.Canceled):
		code = "CANCELLED"
	case errors.Is(err, context.DeadlineExceeded):
		code = "TIMEOUT"
	case errors.Is(err, ErrForbidden):
		code = "PERMISSION_DENIED"
	case errors.Is(err, ErrBadInput):
		code = "INVALID_ARGUMENT"
	case errors.Is(err, ErrNotFound):
		code = "NOT_FOUND"
	}
	meta := map[string]any{"errorCode": code, "retryable": retryable, "outcomeKnown": code != "TIMEOUT" && code != "CANCELLED"}
	switch code {
	case "PERMISSION_DENIED":
		meta["nextSteps"] = []string{"Run app.preflight for the exact tool and arguments to inspect missing capabilities and target/path scopes."}
	case "INVALID_ARGUMENT":
		meta["nextSteps"] = []string{"Read the tool input schema with tools.search, correct the arguments, then run app.preflight."}
	case "NOT_FOUND":
		meta["nextSteps"] = []string{"Refresh the relevant session, server or job state and use the returned identifier."}
	case "TIMEOUT", "CANCELLED":
		meta["nextSteps"] = []string{"Inspect the operation state before retrying; a timed out request does not prove that its side effects stopped."}
	}
	return meta
}
