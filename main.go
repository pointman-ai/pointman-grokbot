// grokbot-wake hands one agent wake to a Grok Bot routine through its webhook trigger.
//
// The node runs it with the wake's prompt on stdin and the routine's webhook in GROKBOT_WEBHOOK, as "<url> <key>"
// (sealed in the vault by `/grokbot add`). It POSTs {"source": "pointman", "prompt": …} to the URL with the key as a
// bearer token. A 2xx answer means Grok Bot started the run, which answers on the board with Pointman's tools itself,
// so this exits 0 at once. Anything else exits 1 with the reason on stderr. The key and the URL's path never reach
// stdout or stderr: only the URL's host does.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

var version = "dev"

const (
	envName   = "GROKBOT_WEBHOOK"
	maxPrompt = 4 << 20 // the node's prompts are tens of KB
	timeout   = 60 * time.Second
)

// hook is a routine's webhook: where to POST and the key it takes.
type hook struct {
	url *url.URL
	key string
}

// parseHook reads "<https url> <key>", the one vault value `/grokbot add` seals.
func parseHook(v string) (hook, error) {
	if strings.TrimSpace(v) == "" {
		return hook{}, errors.New("no Grok Bot webhook on this machine: add the routine's webhook with /grokbot add")
	}
	parts := strings.Fields(v)
	if len(parts) != 2 {
		return hook{}, errors.New("the saved Grok Bot webhook should be its address, a space, then its key: add it again with /grokbot add")
	}
	u, err := url.Parse(parts[0])
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return hook{}, errors.New("the saved Grok Bot webhook address isn't an https:// link: add it again with /grokbot add")
	}
	return hook{url: u, key: parts[1]}, nil
}

// send POSTs the prompt and says what happened: a line for the run's result, or an error for its failure.
func send(ctx context.Context, c *http.Client, h hook, prompt string) (string, error) {
	body, err := json.Marshal(map[string]string{"source": "pointman", "prompt": prompt})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.url.String(), bytes.NewReader(body))
	if err != nil {
		return "", errors.New("the saved Grok Bot webhook address can't be used: add it again with /grokbot add")
	}
	req.Header.Set("Authorization", "Bearer "+h.key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "pointman-grokbot/"+version)
	resp, err := c.Do(req)
	if err != nil {
		// url.Error carries the whole URL: keep only why
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		return "", fmt.Errorf("couldn't reach Grok Bot's webhook at %s: %v", h.url.Host, err)
	}
	defer resp.Body.Close()
	said, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return fmt.Sprintf("Sent to Grok Bot (%s): it answers on the board itself.", h.url.Host), nil
	}
	return "", fmt.Errorf("%s (HTTP %d%s)", why(resp.StatusCode), resp.StatusCode, quote(said, h.key))
}

// why says what a refusal means for the person, in the words the node's limit check looks for where it's a limit.
func why(status int) string {
	switch {
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "Grok Bot refused the webhook key: add the routine's current key with /grokbot add"
	case status == http.StatusNotFound:
		return "Grok Bot has no routine at that webhook: check the routine still has its webhook trigger, then /grokbot add"
	case status == http.StatusTooManyRequests:
		return "Grok Bot's rate limit for routine runs was reached"
	case status >= 500:
		return "Grok Bot's webhook failed on its side"
	default:
		return "Grok Bot didn't start the routine: check it isn't paused"
	}
}

// quote is what the webhook answered, on one line and short, with the key taken out if it's in there.
func quote(said []byte, key string) string {
	s := strings.Join(strings.Fields(string(said)), " ")
	if key != "" {
		s = strings.ReplaceAll(s, key, "…")
	}
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	if s == "" {
		return ""
	}
	return ": " + s
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("grokbot-wake " + version)
		return
	}
	h, err := parseHook(os.Getenv(envName))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	prompt, err := io.ReadAll(io.LimitReader(os.Stdin, maxPrompt+1))
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading the wake's prompt:", err)
		os.Exit(1)
	}
	if len(prompt) > maxPrompt {
		fmt.Fprintln(os.Stderr, "the wake's prompt is over 4 MB: not sent")
		os.Exit(1)
	}
	if len(bytes.TrimSpace(prompt)) == 0 {
		fmt.Fprintln(os.Stderr, "the wake's prompt is empty: not sent")
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	line, err := send(ctx, http.DefaultClient, h, string(prompt))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println(line)
}
