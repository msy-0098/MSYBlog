package mail

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"image/png"
	"io"
	"mime"
	"mime/multipart"
	stdmail "net/mail"
	"os"
	"strings"
	"testing"
	"time"
)

func TestSMTPSenderHonorsCanceledContextBeforeConnecting(t *testing.T) {
	sender, err := NewSMTPSender("smtp.example.test", "587", "sender@example.com", "secret", "sender@example.com")
	if err != nil {
		t.Fatalf("NewSMTPSender: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := sender.Send(ctx, "reader@example.com", []byte("message")); err != context.Canceled {
		t.Fatalf("Send error = %v, want context.Canceled", err)
	}
}

func TestNewSMTPSenderRejectsUnsafeConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		host     string
		port     string
		username string
		password string
		from     string
	}{
		{name: "host injection", host: "smtp.example.test\r\nDATA", port: "587", username: "sender@example.com", password: "secret", from: "sender@example.com"},
		{name: "invalid port", host: "smtp.example.test", port: "70000", username: "sender@example.com", password: "secret", from: "sender@example.com"},
		{name: "username injection", host: "smtp.example.test", port: "587", username: "sender@example.com\n", password: "secret", from: "sender@example.com"},
		{name: "invalid from", host: "smtp.example.test", port: "587", username: "sender@example.com", password: "secret", from: "not-an-email"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewSMTPSender(test.host, test.port, test.username, test.password, test.from); err == nil {
				t.Fatal("NewSMTPSender accepted unsafe configuration")
			}
		})
	}
}

type parsedVerificationEmail struct {
	header   stdmail.Header
	plain    string
	html     string
	logo     []byte
	logoHead map[string]string
}

func TestBuildVerificationEmailCreatesRelatedAlternativeWithInlineLogo(t *testing.T) {
	message, err := BuildVerificationEmail(VerificationEmail{
		From:      "sender@example.com",
		To:        "reader@example.com",
		Code:      "123456",
		Purpose:   "register",
		ExpiresIn: 12 * time.Minute,
	})
	if err != nil {
		t.Fatalf("BuildVerificationEmail: %v", err)
	}

	parsed := parseVerificationEmail(t, message)
	for name, body := range map[string]string{"plain": parsed.plain, "html": parsed.html} {
		if !strings.Contains(body, "123456") {
			t.Fatalf("%s body does not contain verification code", name)
		}
		if !strings.Contains(body, "12 分钟") {
			t.Fatalf("%s body does not contain configured expiry: %q", name, body)
		}
		if !strings.Contains(body, "非本人操作") {
			t.Fatalf("%s body does not contain security notice: %q", name, body)
		}
	}
	if !strings.Contains(parsed.html, "cid:msy-logo") || !strings.Contains(parsed.html, `alt="MSY 博客"`) {
		t.Fatalf("html does not reference the embedded logo with accessible alt text: %q", parsed.html)
	}
	if parsed.logoHead["Content-Type"] != "image/png" {
		t.Fatalf("logo Content-Type = %q, want image/png", parsed.logoHead["Content-Type"])
	}
	if parsed.logoHead["Content-ID"] != "<msy-logo>" {
		t.Fatalf("logo Content-ID = %q, want <msy-logo>", parsed.logoHead["Content-ID"])
	}
	if parsed.logoHead["Content-Disposition"] != "inline" {
		t.Fatalf("logo Content-Disposition = %q, want inline", parsed.logoHead["Content-Disposition"])
	}

	wantLogo, err := os.ReadFile("assets/msy-logo.png")
	if err != nil {
		t.Fatalf("read expected logo: %v", err)
	}
	if !bytes.Equal(parsed.logo, wantLogo) {
		t.Fatal("decoded inline logo differs from embedded asset")
	}
	if _, err := png.Decode(bytes.NewReader(parsed.logo)); err != nil {
		t.Fatalf("inline logo is not a valid PNG: %v", err)
	}
}

func TestBuildVerificationEmailUsesRandomBoundaries(t *testing.T) {
	input := VerificationEmail{
		From:      "sender@example.com",
		To:        "reader@example.com",
		Code:      "123456",
		Purpose:   "register",
		ExpiresIn: 10 * time.Minute,
	}
	first, err := BuildVerificationEmail(input)
	if err != nil {
		t.Fatalf("build first email: %v", err)
	}
	second, err := BuildVerificationEmail(input)
	if err != nil {
		t.Fatalf("build second email: %v", err)
	}
	firstMessage, err := stdmail.ReadMessage(bytes.NewReader(first))
	if err != nil {
		t.Fatalf("parse first email: %v", err)
	}
	secondMessage, err := stdmail.ReadMessage(bytes.NewReader(second))
	if err != nil {
		t.Fatalf("parse second email: %v", err)
	}
	if firstMessage.Header.Get("Content-Type") == secondMessage.Header.Get("Content-Type") {
		t.Fatal("separate emails reused the same multipart boundary")
	}
}

func TestBuildVerificationEmailEncodesUTF8SubjectAndPurposeCopy(t *testing.T) {
	tests := []struct {
		purpose string
		want    string
	}{
		{purpose: "register", want: "注册"},
		{purpose: "reset", want: "重置密码"},
	}

	for _, test := range tests {
		t.Run(test.purpose, func(t *testing.T) {
			message, err := BuildVerificationEmail(VerificationEmail{
				From:      "sender@example.com",
				To:        "reader@example.com",
				Code:      "654321",
				Purpose:   test.purpose,
				ExpiresIn: 90 * time.Second,
			})
			if err != nil {
				t.Fatalf("BuildVerificationEmail: %v", err)
			}

			parsed := parseVerificationEmail(t, message)
			subject, err := new(mime.WordDecoder).DecodeHeader(parsed.header.Get("Subject"))
			if err != nil {
				t.Fatalf("decode Subject: %v", err)
			}
			if !strings.Contains(subject, test.want) {
				t.Fatalf("decoded Subject = %q, want purpose text %q", subject, test.want)
			}
			if !strings.Contains(parsed.plain, test.want) || !strings.Contains(parsed.html, test.want) {
				t.Fatalf("purpose copy %q missing from one of the bodies", test.want)
			}
			if !strings.Contains(parsed.plain, "90 秒") || !strings.Contains(parsed.html, "90 秒") {
				t.Fatal("dynamic second-based expiry missing from bodies")
			}
		})
	}
}

func TestBuildVerificationEmailEncodesUTF8DisplayNames(t *testing.T) {
	message, err := BuildVerificationEmail(VerificationEmail{
		From:      "马森雨 <sender@example.com>",
		To:        "读者 <reader@example.com>",
		Code:      "123456",
		Purpose:   "register",
		ExpiresIn: 10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("BuildVerificationEmail: %v", err)
	}
	parsed, err := stdmail.ReadMessage(bytes.NewReader(message))
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	for header, wantName := range map[string]string{"From": "马森雨", "To": "读者"} {
		raw := parsed.Header.Get(header)
		if strings.Contains(raw, wantName) {
			t.Fatalf("%s header contains an unencoded UTF-8 display name: %q", header, raw)
		}
		address, err := stdmail.ParseAddress(raw)
		if err != nil {
			t.Fatalf("parse %s address: %v", header, err)
		}
		if address.Name != wantName {
			t.Fatalf("decoded %s display name = %q, want %q", header, address.Name, wantName)
		}
	}
}

func TestBuildVerificationEmailWrapsBase64At76CharactersWithCRLF(t *testing.T) {
	message, err := BuildVerificationEmail(VerificationEmail{
		From:      "sender@example.com",
		To:        "reader@example.com",
		Code:      "123456",
		Purpose:   "register",
		ExpiresIn: 10 * time.Minute,
	})
	if err != nil {
		t.Fatalf("BuildVerificationEmail: %v", err)
	}

	marker := []byte("Content-Transfer-Encoding: base64\r\n")
	start := bytes.Index(message, marker)
	if start < 0 {
		t.Fatal("inline image base64 section not found")
	}
	encoded := message[start+len(marker):]
	bodyStart := bytes.Index(encoded, []byte("\r\n\r\n"))
	if bodyStart < 0 {
		t.Fatal("inline image MIME headers are not terminated")
	}
	encoded = encoded[bodyStart+4:]
	end := bytes.Index(encoded, []byte("\r\n--"))
	if end < 0 {
		t.Fatal("inline image closing boundary not found")
	}
	for index, line := range bytes.Split(encoded[:end], []byte("\r\n")) {
		if len(line) > 76 {
			t.Fatalf("base64 line %d has %d characters, want <= 76", index+1, len(line))
		}
		if bytes.Contains(line, []byte{'\n'}) || bytes.Contains(line, []byte{'\r'}) {
			t.Fatalf("base64 line %d contains a bare newline", index+1)
		}
	}
}

func TestBuildVerificationEmailRejectsUnsafeInputs(t *testing.T) {
	base := VerificationEmail{
		From:      "sender@example.com",
		To:        "reader@example.com",
		Code:      "123456",
		Purpose:   "register",
		ExpiresIn: 10 * time.Minute,
	}
	tests := map[string]func(*VerificationEmail){
		"from header injection":    func(email *VerificationEmail) { email.From += "\r\nBcc: victim@example.com" },
		"to header injection":      func(email *VerificationEmail) { email.To += "\nBcc: victim@example.com" },
		"subject header injection": func(email *VerificationEmail) { email.Subject = "hello\r\nBcc: victim@example.com" },
		"unknown purpose":          func(email *VerificationEmail) { email.Purpose = "login" },
		"unsafe code":              func(email *VerificationEmail) { email.Code = "123456\r\nBcc: victim@example.com" },
		"invalid expiry":           func(email *VerificationEmail) { email.ExpiresIn = 0 },
	}

	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			input := base
			mutate(&input)
			if _, err := BuildVerificationEmail(input); err == nil {
				t.Fatal("BuildVerificationEmail succeeded for unsafe input")
			}
		})
	}
}

func parseVerificationEmail(t *testing.T, raw []byte) parsedVerificationEmail {
	t.Helper()

	message, err := stdmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("parse message: %v", err)
	}
	mediaType, params, err := mime.ParseMediaType(message.Header.Get("Content-Type"))
	if err != nil {
		t.Fatalf("parse outer Content-Type: %v", err)
	}
	if mediaType != "multipart/related" {
		t.Fatalf("outer Content-Type = %q, want multipart/related", mediaType)
	}

	result := parsedVerificationEmail{header: message.Header}
	related := multipart.NewReader(message.Body, params["boundary"])
	for {
		part, err := related.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read related part: %v", err)
		}
		partType, partParams, err := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if err != nil {
			t.Fatalf("parse related part Content-Type: %v", err)
		}
		switch partType {
		case "multipart/alternative":
			alternative := multipart.NewReader(part, partParams["boundary"])
			for {
				bodyPart, err := alternative.NextPart()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("read alternative part: %v", err)
				}
				body, err := io.ReadAll(bodyPart)
				if err != nil {
					t.Fatalf("read body part: %v", err)
				}
				bodyType, _, _ := mime.ParseMediaType(bodyPart.Header.Get("Content-Type"))
				switch bodyType {
				case "text/plain":
					result.plain = string(body)
				case "text/html":
					result.html = string(body)
				}
			}
		case "image/png":
			encoded, err := io.ReadAll(part)
			if err != nil {
				t.Fatalf("read logo part: %v", err)
			}
			compact := strings.NewReplacer("\r", "", "\n", "").Replace(string(encoded))
			result.logo, err = base64.StdEncoding.DecodeString(compact)
			if err != nil {
				t.Fatalf("decode logo: %v", err)
			}
			result.logoHead = map[string]string{
				"Content-Type":        part.Header.Get("Content-Type"),
				"Content-ID":          part.Header.Get("Content-ID"),
				"Content-Disposition": part.Header.Get("Content-Disposition"),
			}
		default:
			t.Fatalf("unexpected related part type %q", partType)
		}
	}

	if result.plain == "" || result.html == "" || len(result.logo) == 0 {
		t.Fatalf("message is incomplete: plain=%t html=%t logo=%d", result.plain != "", result.html != "", len(result.logo))
	}
	if got := result.header.Get("MIME-Version"); got != "1.0" {
		t.Fatalf("MIME-Version = %q, want 1.0", got)
	}
	if got := result.header.Get("From"); got == "" {
		t.Fatal("From header missing")
	}
	if got := result.header.Get("To"); got == "" {
		t.Fatal("To header missing")
	}
	return result
}

func ExampleBuildVerificationEmail() {
	message, _ := BuildVerificationEmail(VerificationEmail{
		From:      "sender@example.com",
		To:        "reader@example.com",
		Code:      "123456",
		Purpose:   "register",
		ExpiresIn: 10 * time.Minute,
	})
	fmt.Println(len(message) > 0)
	// Output: true
}
