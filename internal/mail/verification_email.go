package mail

import (
	"bytes"
	_ "embed"
	"encoding/base64"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"net/textproto"
	"regexp"
	"strings"
	"time"
)

//go:embed assets/msy-logo.png
var msyLogoPNG []byte

var verificationCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

type VerificationEmail struct {
	From      string
	To        string
	Subject   string
	Code      string
	Purpose   string
	ExpiresIn time.Duration
}

func BuildVerificationEmail(email VerificationEmail) ([]byte, error) {
	fromHeader, err := formatMailboxHeader(email.From)
	if err != nil {
		return nil, fmt.Errorf("invalid From header")
	}
	toHeader, err := formatMailboxHeader(email.To)
	if err != nil {
		return nil, fmt.Errorf("invalid To header")
	}
	if hasHeaderNewline(email.Subject) {
		return nil, fmt.Errorf("invalid Subject header")
	}
	if !verificationCodePattern.MatchString(email.Code) {
		return nil, fmt.Errorf("invalid verification code")
	}
	if email.ExpiresIn <= 0 {
		return nil, fmt.Errorf("invalid verification code expiry")
	}

	copy, err := verificationCopy(email.Purpose)
	if err != nil {
		return nil, err
	}
	subject := strings.TrimSpace(email.Subject)
	if subject == "" {
		subject = copy.subject
	}

	plainText := fmt.Sprintf(
		"MSY 博客\r\n\r\n%s\r\n%s\r\n\r\n验证码：%s\r\n有效期：%s\r\n\r\n安全提醒：若非本人操作，请忽略此邮件，切勿向他人泄露验证码。\r\n",
		copy.title,
		copy.description,
		email.Code,
		expiryLabel(email.ExpiresIn),
	)
	htmlBody := buildHTMLBody(copy, email.Code, expiryLabel(email.ExpiresIn))

	var alternativeBody bytes.Buffer
	alternative := multipart.NewWriter(&alternativeBody)
	plainHeader := make(textproto.MIMEHeader)
	plainHeader.Set("Content-Type", "text/plain; charset=UTF-8")
	plainPart, err := alternative.CreatePart(plainHeader)
	if err != nil {
		return nil, fmt.Errorf("create plain text part: %w", err)
	}
	if _, err := io.WriteString(plainPart, plainText); err != nil {
		return nil, fmt.Errorf("write plain text part: %w", err)
	}
	htmlHeader := make(textproto.MIMEHeader)
	htmlHeader.Set("Content-Type", "text/html; charset=UTF-8")
	htmlPart, err := alternative.CreatePart(htmlHeader)
	if err != nil {
		return nil, fmt.Errorf("create HTML part: %w", err)
	}
	if _, err := io.WriteString(htmlPart, htmlBody); err != nil {
		return nil, fmt.Errorf("write HTML part: %w", err)
	}
	if err := alternative.Close(); err != nil {
		return nil, fmt.Errorf("close alternative multipart: %w", err)
	}

	var body bytes.Buffer
	related := multipart.NewWriter(&body)
	alternativeHeader := make(textproto.MIMEHeader)
	alternativeHeader.Set("Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": alternative.Boundary()}))
	alternativePart, err := related.CreatePart(alternativeHeader)
	if err != nil {
		return nil, fmt.Errorf("create alternative part: %w", err)
	}
	if _, err := alternativePart.Write(alternativeBody.Bytes()); err != nil {
		return nil, fmt.Errorf("write alternative part: %w", err)
	}

	logoHeader := make(textproto.MIMEHeader)
	logoHeader.Set("Content-Type", "image/png")
	logoHeader.Set("Content-ID", "<msy-logo>")
	logoHeader.Set("Content-Disposition", "inline")
	logoHeader.Set("Content-Transfer-Encoding", "base64")
	logoPart, err := related.CreatePart(logoHeader)
	if err != nil {
		return nil, fmt.Errorf("create logo part: %w", err)
	}
	if err := writeBase64Lines(logoPart, msyLogoPNG); err != nil {
		return nil, fmt.Errorf("write logo part: %w", err)
	}
	if err := related.Close(); err != nil {
		return nil, fmt.Errorf("close related multipart: %w", err)
	}

	var message bytes.Buffer
	fmt.Fprintf(&message, "From: %s\r\n", fromHeader)
	fmt.Fprintf(&message, "To: %s\r\n", toHeader)
	fmt.Fprintf(&message, "Subject: %s\r\n", mime.BEncoding.Encode("UTF-8", subject))
	message.WriteString("MIME-Version: 1.0\r\n")
	fmt.Fprintf(&message, "Content-Type: %s\r\n\r\n", mime.FormatMediaType("multipart/related", map[string]string{"boundary": related.Boundary()}))
	message.Write(body.Bytes())
	return message.Bytes(), nil
}

type emailCopy struct {
	subject     string
	title       string
	description string
}

func verificationCopy(purpose string) (emailCopy, error) {
	switch purpose {
	case "register":
		return emailCopy{
			subject:     "【MSY 博客】注册验证码",
			title:       "注册账号",
			description: "你正在注册 MSY 博客账号，请使用以下验证码完成验证。",
		}, nil
	case "reset":
		return emailCopy{
			subject:     "【MSY 博客】重置密码验证码",
			title:       "重置密码",
			description: "你正在重置 MSY 博客账号密码，请使用以下验证码完成验证。",
		}, nil
	default:
		return emailCopy{}, fmt.Errorf("invalid verification email purpose")
	}
}

func buildHTMLBody(copy emailCopy, code, expires string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="zh-CN">
<head><meta charset="UTF-8"><title>%s</title></head>
<body style="margin:0;padding:0;background:#f5f6f8;color:#202124;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','Microsoft YaHei',sans-serif">
  <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="padding:32px 16px;background:#f5f6f8">
    <tr><td align="center">
      <table role="presentation" width="100%%" cellspacing="0" cellpadding="0" style="max-width:560px;background:#fff;border:1px solid #e5e7eb">
        <tr><td style="padding:24px 32px;border-bottom:1px solid #e5e7eb"><img src="cid:msy-logo" alt="MSY 博客" width="164" style="display:block;width:164px;height:auto"></td></tr>
        <tr><td style="padding:32px">
          <h1 style="margin:0 0 12px;font-size:22px;line-height:1.4">%s</h1>
          <p style="margin:0 0 24px;color:#5f6368;font-size:14px;line-height:1.7">%s</p>
          <div style="padding:20px;text-align:center;background:#f3f4f6;border:1px solid #d1d5db;font-family:Consolas,'Courier New',monospace;font-size:32px;font-weight:700;letter-spacing:8px">%s</div>
          <p style="margin:24px 0 8px;color:#5f6368;font-size:13px;line-height:1.6">验证码有效期为 <strong>%s</strong>。</p>
          <p style="margin:0;color:#6b7280;font-size:13px;line-height:1.6">安全提醒：若非本人操作，请忽略此邮件，切勿向他人泄露验证码。</p>
        </td></tr>
        <tr><td style="padding:18px 32px;background:#f9fafb;color:#9ca3af;font-size:12px;text-align:center">此邮件由 masenyu.top 系统自动发送，请勿直接回复。</td></tr>
      </table>
    </td></tr>
  </table>
</body>
</html>`, html.EscapeString(copy.subject), html.EscapeString(copy.title), html.EscapeString(copy.description), html.EscapeString(code), html.EscapeString(expires))
}

func expiryLabel(expiresIn time.Duration) string {
	seconds := int64((expiresIn + time.Second - 1) / time.Second)
	if seconds%60 == 0 {
		return fmt.Sprintf("%d 分钟", seconds/60)
	}
	return fmt.Sprintf("%d 秒", seconds)
}

func writeBase64Lines(writer io.Writer, data []byte) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	for len(encoded) > 76 {
		if _, err := io.WriteString(writer, encoded[:76]+"\r\n"); err != nil {
			return err
		}
		encoded = encoded[76:]
	}
	_, err := io.WriteString(writer, encoded)
	return err
}
