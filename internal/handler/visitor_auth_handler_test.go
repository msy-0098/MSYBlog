package handler

import (
	"bufio"
	"fmt"
	"net"
	"net/mail"
	"strings"
	"testing"

	"masenyu.top/blog/backend/internal/config"
)

func TestSendCodeEmailUsesRegistrationEmailAsRecipient(t *testing.T) {
	host, port := startSMTPServerRequiringHeaders(t, "sender@example.com", "reader@example.com")
	handler := VisitorAuthHandler{
		cfg: config.Config{
			Mail: config.MailConfig{
				SMTPHost: host,
				SMTPPort: port,
				Username: "sender@example.com",
				Password: "smtp-password",
				From:     "sender@example.com",
			},
		},
	}

	if err := handler.sendCodeEmail("reader@example.com", "123456", "register"); err != nil {
		t.Fatalf("expected email to be addressed to registration email, got %v", err)
	}
}

func startSMTPServerRequiringHeaders(t *testing.T, expectedFrom string, expectedTo string) (string, string) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen smtp server: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			done <- err
			return
		}
		defer conn.Close()
		done <- handleSMTPConnection(conn, expectedFrom, expectedTo)
	}()

	t.Cleanup(func() {
		_ = listener.Close()
		if err := <-done; err != nil && err.Error() != "EOF" && !strings.Contains(err.Error(), "use of closed network connection") {
			t.Fatalf("smtp test server: %v", err)
		}
	})

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split smtp address: %v", err)
	}

	return "localhost", port
}

func handleSMTPConnection(conn net.Conn, expectedFrom string, expectedTo string) error {
	reader := bufio.NewReader(conn)
	writer := bufio.NewWriter(conn)
	var recipient string
	writeLine := func(line string) error {
		if _, err := writer.WriteString(line + "\r\n"); err != nil {
			return err
		}
		return writer.Flush()
	}

	if err := writeLine("220 localhost ESMTP"); err != nil {
		return err
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		command := strings.TrimRight(line, "\r\n")
		upper := strings.ToUpper(command)

		switch {
		case strings.HasPrefix(upper, "EHLO") || strings.HasPrefix(upper, "HELO"):
			if _, err := writer.WriteString("250-localhost\r\n250-AUTH PLAIN\r\n250 OK\r\n"); err != nil {
				return err
			}
			if err := writer.Flush(); err != nil {
				return err
			}
		case strings.HasPrefix(upper, "AUTH PLAIN"):
			if err := writeLine("235 2.7.0 Authentication successful"); err != nil {
				return err
			}
		case strings.HasPrefix(upper, "MAIL FROM:"):
			if err := writeLine("250 OK"); err != nil {
				return err
			}
		case strings.HasPrefix(upper, "RCPT TO:"):
			recipient = extractSMTPAddress(command)
			if recipient != expectedTo {
				return fmt.Errorf("expected smtp envelope recipient %q, got %q", expectedTo, recipient)
			}
			if err := writeLine("250 OK"); err != nil {
				return err
			}
		case strings.HasPrefix(upper, "DATA"):
			if err := writeLine("354 End data with <CR><LF>.<CR><LF>"); err != nil {
				return err
			}
			message, err := readSMTPData(reader)
			if err != nil {
				return err
			}
			parsed, err := mail.ReadMessage(strings.NewReader(message))
			if err != nil || parsed.Header.Get("From") != expectedFrom || parsed.Header.Get("To") != expectedTo {
				if err := writeLine(`550 The "From" or "To" header is missing or invalid`); err != nil {
					return err
				}
				continue
			}
			if err := writeLine("250 OK"); err != nil {
				return err
			}
		case strings.HasPrefix(upper, "QUIT"):
			return writeLine("221 Bye")
		default:
			return fmt.Errorf("unexpected smtp command %q", command)
		}
	}
}

func extractSMTPAddress(command string) string {
	start := strings.Index(command, "<")
	end := strings.LastIndex(command, ">")
	if start >= 0 && end > start {
		return command[start+1 : end]
	}

	_, value, ok := strings.Cut(command, ":")
	if !ok {
		return ""
	}

	return strings.TrimSpace(value)
}

func readSMTPData(reader *bufio.Reader) (string, error) {
	var builder strings.Builder
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", err
		}
		if line == ".\r\n" {
			return builder.String(), nil
		}
		builder.WriteString(line)
	}
}
