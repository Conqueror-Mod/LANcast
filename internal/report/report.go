// Package report sends a problem report to the developer (ADR 0109): only
// when a person presses Send, only what the form showed them, and only to the
// one webhook a release build carries.
package report

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
	"time"
)

/*
 * WebhookURL is where reports go: a Discord webhook in the developer's private
 * channel, stamped into release builds by
 *
 *	-ldflags -X lancast/internal/report.WebhookURL=…
 *
 * from a GitHub Actions secret, never written in the repository. Discord is a
 * GitHub secret-scanning partner, so a webhook URL pushed to a public repo is
 * revoked within minutes, and scrapers that find one before that spam it.
 *
 * A build without it, any local or self-built one, cannot send: the form
 * says so and offers to copy the report instead. LANCAST_REPORT_WEBHOOK
 * overrides it, for a self-builder who wants reports somewhere of their own
 * and for testing against a local listener.
 */
var WebhookURL = ""

// Webhook is the URL in effect, or "" when this build cannot send.
func Webhook() string {
	if v := strings.TrimSpace(os.Getenv("LANCAST_REPORT_WEBHOOK")); v != "" {
		return v
	}
	return WebhookURL
}

// Report is what the person wrote, and what goes with it.
type Report struct {
	What     string  // what happened
	Expected string  // what they expected
	Steps    string  // how to make it happen again
	Contact  string  // how to reach them, optional
	Version  string  // LANcast's version
	Platform string  // OS and architecture of the server
	Client   string  // which client they were using, as the client says
	Account  string  // "admin" or "member", never a name
	Log      []byte  // the end of the server log, when they chose to include it
	Images   []Image // pictures they attached, already made safe by PrepareImage
}

// Limits on what one report carries. Discord caps a message at 2,000
// characters and a webhook's files at 10 MB; the text fields are capped well
// below so the summary always fits, and the full text rides as a file.
const (
	MaxField    = 4000
	MaxLogBytes = 1 << 20
	contentCap  = 1900
)

// ErrNoWebhook is a build that cannot send.
var ErrNoWebhook = errors.New("report: this build has no destination for reports")

// Body is the report as text: what the summary is cut from and what is
// attached whole.
func (r Report) Body() string {
	var b strings.Builder
	section := func(title, text string) {
		text = strings.TrimSpace(text)
		if text == "" {
			return
		}
		fmt.Fprintf(&b, "**%s**\n%s\n\n", title, text)
	}
	section("What happened", r.What)
	section("Expected", r.Expected)
	section("Steps", r.Steps)
	section("Contact", r.Contact)
	fmt.Fprintf(&b, "LANcast %s · %s · %s · %s", or(r.Version, "unknown"), or(r.Platform, "unknown"),
		or(r.Client, "unknown client"), or(r.Account, "unknown account"))
	if len(r.Log) > 0 {
		b.WriteString(" · log attached")
	}
	switch n := len(r.Images); {
	case n == 1:
		b.WriteString(" · 1 picture")
	case n > 1:
		fmt.Fprintf(&b, " · %d pictures", n)
	}
	return b.String()
}

func or(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}

// Send posts the report to url as one Discord message: the text, cut to fit
// the message, with the full text, the log and any pictures attached as files.
func Send(ctx context.Context, client *http.Client, url string, r Report) error {
	if url == "" {
		return ErrNoWebhook
	}
	body := r.Body()
	content := body
	if len([]rune(content)) > contentCap {
		content = string([]rune(content)[:contentCap]) + "… (full text attached)"
	}

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	payload, _ := json.Marshal(map[string]any{
		"username": "LANcast report",
		"content":  content,
		// A report is text somebody typed: it must not be able to ping anyone.
		"allowed_mentions": map[string]any{"parse": []string{}},
	})
	if err := mw.WriteField("payload_json", string(payload)); err != nil {
		return err
	}
	n := 0
	attach := func(name string, data []byte) error {
		w, err := mw.CreateFormFile(fmt.Sprintf("files[%d]", n), name)
		if err != nil {
			return err
		}
		n++
		_, err = w.Write(data)
		return err
	}
	if content != body {
		if err := attach("report.txt", []byte(body)); err != nil {
			return err
		}
	}
	if len(r.Log) > 0 {
		if err := attach("lancastd-tail.log", r.Log); err != nil {
			return err
		}
	}
	// Discord shows an attached picture in the message itself.
	for _, img := range r.Images {
		if err := attach(img.Name, img.Data); err != nil {
			return err
		}
	}
	if err := mw.Close(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("report: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("report: the destination answered %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return nil
}
