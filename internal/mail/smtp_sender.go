package mail

import (
	"context"
	"fmt"
	"net"
	stdmail "net/mail"
	"net/smtp"
	"strconv"
	"strings"
)

type SMTPSender struct {
	host     string
	address  string
	username string
	password string
	from     string
}

func NewSMTPSender(host, port, username, password, from string) (*SMTPSender, error) {
	if hasHeaderNewline(host) || hasHeaderNewline(port) || hasHeaderNewline(username) || hasHeaderNewline(from) {
		return nil, fmt.Errorf("invalid SMTP configuration")
	}
	host = strings.TrimSpace(host)
	port = strings.TrimSpace(port)
	username = strings.TrimSpace(username)
	from = strings.TrimSpace(from)
	if host == "" || port == "" || username == "" || password == "" || from == "" {
		return nil, fmt.Errorf("incomplete SMTP configuration")
	}
	portNumber, err := strconv.Atoi(port)
	if err != nil || portNumber < 1 || portNumber > 65535 {
		return nil, fmt.Errorf("invalid SMTP port")
	}
	fromAddress, err := parseMailbox(from)
	if err != nil {
		return nil, fmt.Errorf("invalid SMTP sender address")
	}

	return &SMTPSender{
		host:     host,
		address:  net.JoinHostPort(host, port),
		username: username,
		password: password,
		from:     fromAddress,
	}, nil
}

// Send checks ctx before dialing. net/smtp exposes only a synchronous SendMail
// operation, so cancellation cannot interrupt a delivery already in progress.
func (s *SMTPSender) Send(ctx context.Context, to string, message []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	toAddress, err := parseMailbox(to)
	if err != nil {
		return fmt.Errorf("invalid recipient address")
	}
	authenticator := smtp.PlainAuth("", s.username, s.password, s.host)
	if err := smtp.SendMail(s.address, authenticator, s.from, []string{toAddress}, message); err != nil {
		return fmt.Errorf("send verification email: %w", err)
	}
	return nil
}

func parseMailbox(value string) (string, error) {
	address, err := parseMailboxAddress(value)
	if err != nil {
		return "", err
	}
	return address.Address, nil
}

func formatMailboxHeader(value string) (string, error) {
	address, err := parseMailboxAddress(value)
	if err != nil {
		return "", err
	}
	return address.String(), nil
}

func parseMailboxAddress(value string) (*stdmail.Address, error) {
	if hasHeaderNewline(value) {
		return nil, fmt.Errorf("mailbox contains a newline")
	}
	address, err := stdmail.ParseAddress(strings.TrimSpace(value))
	if err != nil || address.Address == "" {
		return nil, fmt.Errorf("invalid mailbox")
	}
	return address, nil
}

func hasHeaderNewline(value string) bool {
	return strings.ContainsAny(value, "\r\n")
}
