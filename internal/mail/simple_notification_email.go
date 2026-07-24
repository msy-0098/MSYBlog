package mail

import (
	"bytes"
	"fmt"
	"mime"
	"net/mail"
	"strings"
	"time"
)

// SimpleNotificationEmail is a lightweight HTML+text notification message.
type SimpleNotificationEmail struct {
	From        string
	To          string
	Subject     string
	Title       string
	Body        string
	ActionURL   string
	ActionLabel string
}

// BuildSimpleNotificationEmail builds a multipart/alternative message.
func BuildSimpleNotificationEmail(input SimpleNotificationEmail) ([]byte, error) {
	from := strings.TrimSpace(input.From)
	to := strings.TrimSpace(input.To)
	if from == "" || to == "" {
		return nil, fmt.Errorf("from and to are required")
	}
	if _, err := mail.ParseAddress(from); err != nil {
		return nil, fmt.Errorf("invalid from address: %w", err)
	}
	if _, err := mail.ParseAddress(to); err != nil {
		return nil, fmt.Errorf("invalid to address: %w", err)
	}

	subject := strings.TrimSpace(input.Subject)
	if subject == "" {
		subject = "站点通知"
	}
	title := strings.TrimSpace(input.Title)
	if title == "" {
		title = subject
	}
	body := strings.TrimSpace(input.Body)
	actionURL := strings.TrimSpace(input.ActionURL)
	actionLabel := strings.TrimSpace(input.ActionLabel)
	if actionLabel == "" {
		actionLabel = "打开后台"
	}

	plain := title + "\n\n" + body
	if actionURL != "" {
		plain += "\n\n" + actionLabel + ": " + actionURL
	}
	plain += "\n\n— masenyu.top"

	var html bytes.Buffer
	html.WriteString(`<!DOCTYPE html><html><body style="font-family:sans-serif;line-height:1.6;color:#1f2937;">`)
	html.WriteString(`<div style="max-width:520px;margin:0 auto;padding:24px;border:1px solid #e5e7eb;border-radius:12px;">`)
	fmt.Fprintf(&html, `<h2 style="margin:0 0 12px;font-size:18px;">%s</h2>`, htmlEscape(title))
	fmt.Fprintf(&html, `<p style="margin:0 0 16px;color:#4b5563;">%s</p>`, htmlEscape(body))
	if actionURL != "" {
		fmt.Fprintf(
			&html,
			`<p style="margin:0 0 16px;"><a href="%s" style="display:inline-block;padding:10px 16px;background:#2563eb;color:#fff;text-decoration:none;border-radius:8px;">%s</a></p>`,
			htmlEscape(actionURL),
			htmlEscape(actionLabel),
		)
	}
	html.WriteString(`<p style="margin:0;font-size:12px;color:#9ca3af;">可在后台 · 设置中关闭此类邮件 · masenyu.top</p>`)
	html.WriteString(`</div></body></html>`)

	boundary := fmt.Sprintf("msy-notify-%d", time.Now().UnixNano())
	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\n", from)
	fmt.Fprintf(&message, "To: %s\r\n", to)
	fmt.Fprintf(&message, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", subject))
	fmt.Fprintf(&message, "MIME-Version: 1.0\r\n")
	fmt.Fprintf(&message, "Content-Type: multipart/alternative; boundary=%q\r\n\r\n", boundary)
	fmt.Fprintf(&message, "--%s\r\n", boundary)
	message.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	message.WriteString(plain)
	message.WriteString("\r\n")
	fmt.Fprintf(&message, "--%s\r\n", boundary)
	message.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	message.Write(html.Bytes())
	message.WriteString("\r\n")
	fmt.Fprintf(&message, "--%s--\r\n", boundary)
	return message.Bytes(), nil
}

func htmlEscape(value string) string {
	replacer := strings.NewReplacer(
		`&`, "&amp;",
		`<`, "&lt;",
		`>`, "&gt;",
		`"`, "&quot;",
		`'`, "&#39;",
	)
	return replacer.Replace(value)
}