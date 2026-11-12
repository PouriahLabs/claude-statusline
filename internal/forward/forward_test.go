package forward

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAllowedOnlyLoopbackHTTP(t *testing.T) {
	for url, want := range map[string]bool{
		"http://127.0.0.1:53821/statusline": true,
		"http://localhost:53821/statusline": true,
		"http://[::1]:53821/statusline":     true,
		"https://127.0.0.1:53821/x":         false,
		"http://10.0.0.5:53821/x":           false,
		"http://example.com/x":              false,
		"http://127.0.0.1.evil.example/x":   false,
		"not a url":                         false,
	} {
		if got := Allowed(url); got != want {
			t.Errorf("Allowed(%q) = %v, want %v", url, got, want)
		}
	}
}

func TestSendPostsRawPayload(t *testing.T) {
	var gotBody, gotType, gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody, gotType, gotHeader = string(b), r.Header.Get("Content-Type"), r.Header.Get("X-Tokenomics")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	if !Send(srv.URL+"/statusline", []byte(`{"session_id":"s1"}`), time.Second) {
		t.Fatal("Send returned false for a 204 listener")
	}
	if gotBody != `{"session_id":"s1"}` || gotType != "application/json" || gotHeader != "1" {
		t.Fatalf("got body=%q type=%q header=%q", gotBody, gotType, gotHeader)
	}
}

func TestSendGivesUpFastWhenNothingListens(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()

	start := time.Now()
	if Send("http://"+addr+"/statusline", []byte(`{}`), DefaultTimeout) {
		t.Fatal("Send reported success with no listener")
	}
	if d := time.Since(start); d > DefaultTimeout+100*time.Millisecond {
		t.Fatalf("Send took %v, budget is %v", d, DefaultTimeout)
	}
}

func TestTimeoutFallsBack(t *testing.T) {
	if Timeout("") != DefaultTimeout || Timeout("nope") != DefaultTimeout || Timeout("10s") != DefaultTimeout {
		t.Fatal("expected fallback")
	}
	if Timeout("80ms") != 80*time.Millisecond {
		t.Fatal("expected 80ms")
	}
}
