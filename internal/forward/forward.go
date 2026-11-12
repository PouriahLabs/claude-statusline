// Package forward re-posts the status line payload to a local listener.
//
// It runs after the bar is written, inside a short budget, and never reports
// errors: a listener that is down must cost the status line nothing visible.
package forward

import (
	"bytes"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const DefaultTimeout = 150 * time.Millisecond

// Allowed reports whether target is an http URL on a loopback host. The payload
// carries paths and session ids, so it is never sent off the machine.
func Allowed(target string) bool {
	u, err := url.Parse(target)
	if err != nil || u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Timeout parses a duration string, falling back to DefaultTimeout.
func Timeout(s string) time.Duration {
	if d, err := time.ParseDuration(s); err == nil && d > 0 && d <= 2*time.Second {
		return d
	}
	return DefaultTimeout
}

// Send posts body to target as JSON. It returns whether the listener answered 2xx.
func Send(target string, body []byte, timeout time.Duration) bool {
	if !Allowed(target) || len(body) == 0 {
		return false
	}
	client := &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{Proxy: nil, DisableKeepAlives: true},
	}
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tokenomics", "1")
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
