package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

const key = "gbk_test_1234567890"

func TestParseHook(t *testing.T) {
	h, err := parseHook("https://api.example.com/v1/routines/r1/webhook " + key)
	if err != nil {
		t.Fatal(err)
	}
	if h.url.Host != "api.example.com" || h.key != key {
		t.Fatalf("got %v %q", h.url, h.key)
	}
	for _, bad := range []string{
		"", "   ", "https://a.example/hook", "https://a.example/hook k extra",
		"http://a.example/hook " + key, "ftp://a.example " + key, "https:///nohost " + key,
		"https://user:pw@a.example/hook " + key, "notaurl " + key,
	} {
		if _, err := parseHook(bad); err == nil {
			t.Errorf("%q: accepted", bad)
		} else if strings.Contains(err.Error(), key) {
			t.Errorf("%q: error shows the key", bad)
		}
	}
}

func server(t *testing.T, status int, answer string) (*httptest.Server, hook, *http.Request, *[]byte) {
	t.Helper()
	var got http.Request
	var body []byte
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = *r.Clone(context.Background())
		body, _ = io.ReadAll(r.Body)
		w.WriteHeader(status)
		_, _ = io.WriteString(w, answer)
	}))
	t.Cleanup(ts.Close)
	u, _ := url.Parse(ts.URL + "/v1/routines/secret-routine-id/webhook")
	return ts, hook{url: u, key: key}, &got, &body
}

func TestSendStartsTheRoutine(t *testing.T) {
	ts, h, got, body := server(t, 200, `{"ok":true}`)
	line, err := send(context.Background(), ts.Client(), h, "You are @grokbot on Pointman.\nNew posts: …")
	if err != nil {
		t.Fatal(err)
	}
	if got.Method != http.MethodPost || got.URL.Path != "/v1/routines/secret-routine-id/webhook" {
		t.Fatalf("%s %s", got.Method, got.URL.Path)
	}
	if got.Header.Get("Authorization") != "Bearer "+key || got.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("headers %v", got.Header)
	}
	var sent map[string]string
	if err := json.Unmarshal(*body, &sent); err != nil {
		t.Fatal(err)
	}
	if sent["source"] != "pointman" || sent["prompt"] != "You are @grokbot on Pointman.\nNew posts: …" {
		t.Fatalf("body %v", sent)
	}
	if !strings.HasPrefix(line, "Sent to Grok Bot (") || strings.Contains(line, "secret-routine-id") || strings.Contains(line, key) {
		t.Fatalf("line %q", line)
	}
}

func TestRefusalsSayWhyWithoutTheKeyOrPath(t *testing.T) {
	for _, c := range []struct {
		status int
		answer string
		want   string
	}{
		{401, "bad key " + key, "refused the webhook key"},
		{404, "no route /v1/routines/secret-routine-id/webhook", "no routine at that webhook"},
		{404, `{"error":"routine secret-routine-id not found"}`, "no routine at that webhook"},
		{403, "", "refused the webhook key"},
		{404, "not found", "no routine at that webhook"},
		{429, "Automation run rate limit exceeded", "rate limit"},
		{500, "oops", "failed on its side"},
		{409, "routine paused", "isn't paused"},
	} {
		ts, h, _, _ := server(t, c.status, c.answer)
		_, err := send(context.Background(), ts.Client(), h, "hi")
		if err == nil {
			t.Fatalf("%d: no error", c.status)
		}
		msg := err.Error()
		if !strings.Contains(msg, c.want) || !strings.Contains(msg, "HTTP ") {
			t.Errorf("%d: %q", c.status, msg)
		}
		if strings.Contains(msg, key) || strings.Contains(msg, "secret-routine-id") {
			t.Errorf("%d: %q shows the key or the path", c.status, msg)
		}
	}
}

func TestUnreachableSaysOnlyTheHost(t *testing.T) {
	ts, h, _, _ := server(t, 200, "")
	ts.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := send(ctx, ts.Client(), h, "hi")
	if err == nil {
		t.Fatal("no error")
	}
	if strings.Contains(err.Error(), "secret-routine-id") || !strings.Contains(err.Error(), h.url.Host) {
		t.Fatalf("%q", err)
	}
}

func TestRedirectsAreNeverFollowed(t *testing.T) {
	var elsewhere int
	other := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { elsewhere++ }))
	defer other.Close()
	for _, to := range []string{other.URL + "/catch", "/v1/routines/secret-routine-id/webhook2"} {
		var hits int
		ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			hits++
			http.Redirect(w, r, to, http.StatusTemporaryRedirect)
		}))
		u, _ := url.Parse(ts.URL + "/v1/routines/secret-routine-id/webhook")
		c := ts.Client()
		c.Transport.(*http.Transport).TLSClientConfig.InsecureSkipVerify = true // other's certificate too
		_, err := send(context.Background(), c, hook{url: u, key: key}, "hi")
		ts.Close()
		if err == nil || !strings.Contains(err.Error(), "redirect") || !strings.Contains(err.Error(), "HTTP 307") {
			t.Fatalf("%s: %v", to, err)
		}
		if hits != 1 {
			t.Fatalf("%s: followed (%d requests)", to, hits)
		}
	}
	if elsewhere != 0 {
		t.Fatalf("another host got %d requests", elsewhere)
	}
}

func TestQuoteIsShortAndOneLine(t *testing.T) {
	u, _ := url.Parse("https://a.example/v1/routines/secret-routine-id/webhook?sig=abc123")
	h := hook{url: u, key: key}
	q := quote([]byte(strings.Repeat("a\n", 300)), h)
	if strings.Contains(q, "\n") || len(q) > 210 {
		t.Fatalf("%q", q)
	}
	if quote(nil, h) != "" {
		t.Fatal("empty answer quoted")
	}
	q = quote([]byte("at "+u.Path+"?sig=abc123 for secret-routine-id with "+base64.StdEncoding.EncodeToString([]byte(key))), h)
	for _, secret := range []string{"secret-routine-id", "abc123", key, base64.StdEncoding.EncodeToString([]byte(key))} {
		if strings.Contains(q, secret) {
			t.Fatalf("%q shows %q", q, secret)
		}
	}
	q = quote([]byte(strings.Repeat("é", 150)), h)
	if !utf8.ValidString(q) {
		t.Fatalf("cut a character in half: %q", q)
	}
}
