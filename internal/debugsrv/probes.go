package debugsrv

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const OpTargetsProbe = "targets.probe"

func registerProbeCaps() { RegisterToolCap(OpTargetsProbe, CapConnect) }
func probeToolEntries() []mcpTool {
	return []mcpTool{{Name: OpTargetsProbe, Description: "显式探测已授权本机目标的TCP/HTTP/TLS可达性；只供可信本机所有者，需session.connect。固定超时/响应预算，不跟随重定向，不运行shell。SSH远端视角尚不支持。", InputSchema: obj(map[string]any{"host": strProp("精确目标主机"), "port": intProp("TCP/TLS端口1–65535"), "kind": map[string]any{"type": "string", "enum": []string{"tcp", "http", "tls"}}, "url": strProp("HTTP(S)精确URL，禁止URL凭据"), "executionPoint": map[string]any{"type": "string", "enum": []string{"local"}}, "timeoutMs": intProp("默认3000，最多10000"), "expectedStatus": intProp("可选HTTP状态"), "expectedNonce": strProp("可选响应内nonce，最多200字")}, "kind"), OutputSchema: outputObj(nil), structured: structuredIdentity}}
}
func (s *Server) handleProbe(ctx context.Context, args map[string]any) ([]any, error) {
	ms, e := jobArgInt(args, "timeoutMs", 3000, 100, 10000)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(ms)*time.Millisecond)
	defer cancel()
	if p := uiArgString(args, "executionPoint"); p != "" && p != "local" {
		return nil, fmt.Errorf("%w: executionPoint unsupported", ErrBadInput)
	}
	kind := uiArgString(args, "kind")
	host := uiArgString(args, "host")
	port := 0
	if kind != "http" {
		port, e = jobArgInt(args, "port", 0, 1, 65535)
		if e != nil {
			return nil, e
		}
		if strings.TrimSpace(host) == "" || strings.ContainsAny(host, "/\\\x00\r\n") {
			return nil, fmt.Errorf("%w: invalid host", ErrBadInput)
		}
	}
	nonce := uiArgString(args, "expectedNonce")
	if len(nonce) > 200 {
		return nil, ErrBadInput
	}
	expectedStatus := 0
	if kind == "http" && args["expectedStatus"] != nil {
		expectedStatus, e = jobArgInt(args, "expectedStatus", 0, 100, 599)
		if e != nil {
			return nil, e
		}
	}
	start := time.Now()
	result := map[string]any{"kind": kind, "executionPoint": "local", "source": "local-network", "observedAt": start.UnixMilli(), "available": false, "state": "unhealthy", "responseBodyReturned": false}
	var probeErr error
	switch kind {
	case "tcp", "tls":
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		result["target"] = addr
		d := net.Dialer{Timeout: time.Duration(ms) * time.Millisecond}
		var conn net.Conn
		if kind == "tls" {
			td := tls.Dialer{NetDialer: &d, Config: &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12}}
			conn, probeErr = td.DialContext(ctx, "tcp", addr)
		} else {
			conn, probeErr = d.DialContext(ctx, "tcp", addr)
		}
		if conn != nil {
			result["remoteAddress"] = conn.RemoteAddr().String()
			_ = conn.Close()
			result["available"] = true
			result["state"] = "healthy"
		}
	case "http":
		u, e := url.Parse(uiArgString(args, "url"))
		if e != nil || u.User != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return nil, fmt.Errorf("%w: precise http(s) URL without credentials required", ErrBadInput)
		}
		result["target"] = u.Scheme + "://" + u.Host + u.EscapedPath()
		tr := &http.Transport{Proxy: nil, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS12}, ResponseHeaderTimeout: time.Duration(ms) * time.Millisecond, MaxResponseHeaderBytes: 64 << 10}
		defer tr.CloseIdleConnections()
		client := &http.Client{Transport: tr, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		req, requestErr := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if requestErr != nil {
			return nil, fmt.Errorf("%w: invalid HTTP request URL", ErrBadInput)
		}
		response, e := client.Do(req)
		probeErr = e
		if e == nil {
			defer response.Body.Close()
			body, bodyErr := io.ReadAll(io.LimitReader(response.Body, 65537))
			probeErr = bodyErr
			result["statusCode"] = response.StatusCode
			truncated := len(body) > 65536
			complete := bodyErr == nil && !truncated
			result["truncated"] = truncated
			result["responseComplete"] = complete
			statusMatched := expectedStatus == 0 && response.StatusCode >= 200 && response.StatusCode < 400 || expectedStatus != 0 && response.StatusCode == expectedStatus
			nonceMatched := nonce == "" || strings.Contains(string(body), nonce)
			// A bounded prefix does not prove a complete healthy response, even
			// when the nonce happened to arrive before truncation or failure.
			matched := complete && statusMatched && nonceMatched
			result["statusMatched"] = statusMatched
			result["nonceMatched"] = nonceMatched
			result["expectationMatched"] = matched
			result["expectationComplete"] = complete
			result["available"] = true
			if truncated {
				result["errorCode"] = "PROBE_BODY_LIMIT"
			}
			if matched {
				result["state"] = "healthy"
			} else {
				result["state"] = "degraded"
			}
		}
	default:
		return nil, fmt.Errorf("%w: kind must be tcp/http/tls", ErrBadInput)
	}
	if probeErr != nil {
		result["errorCode"] = "PROBE_FAILED"
		if ctx.Err() != nil {
			result["errorCode"] = "PROBE_TIMEOUT"
		}
		// net/http wraps errors in url.Error whose text includes the original
		// request URL, query credentials and sometimes body-controlled text.
		// Preserve typed facts only, never the untrusted error string.
		result["error"] = safeProbeError(probeErr, ctx.Err())
		if result["available"] == true {
			result["state"] = "degraded"
		}
	}
	result["latencyMs"] = time.Since(start).Milliseconds()
	return jobJSON(result)
}

func safeProbeError(err, contextErr error) string {
	if errors.Is(contextErr, context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) {
		return "probe deadline exceeded"
	}
	if errors.Is(contextErr, context.Canceled) || errors.Is(err, context.Canceled) {
		return "probe cancelled"
	}
	var networkErr net.Error
	if errors.As(err, &networkErr) && networkErr.Timeout() {
		return "network operation timed out"
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return "host name resolution failed"
	}
	if errors.Is(err, io.ErrUnexpectedEOF) {
		return "response ended before the declared body was complete"
	}
	return "network, TLS, or response operation failed"
}
