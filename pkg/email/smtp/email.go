package smtp

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/gomail.v2"
)

type Client struct {
	conf   Config
	dailer *gomail.Dialer
}
type Config struct {
	Host               string `json:"host"`
	Port               int    `json:"port"`
	User               string `json:"user"`
	Pass               string `json:"pass"`
	From               string `json:"from"`
	ReplyTo            string `json:"reply_to"`
	SSL                bool   `json:"ssl"`
	ImplicitTLS        bool   `json:"implicit_tls"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify"`
	SiteName           string `json:"siteName"`
}

func NewClient(conf *Config) *Client {
	if conf == nil {
		return nil
	}
	dailer := gomail.NewDialer(conf.Host, conf.Port, conf.User, conf.Pass)
	dailer.SSL = conf.Port == 465 || conf.ImplicitTLS
	dailer.TLSConfig = &tls.Config{
		InsecureSkipVerify: conf.InsecureSkipVerify,
		MinVersion:         tls.VersionTLS12,
		ServerName:         conf.Host,
	}

	return &Client{conf: *conf, dailer: dailer}
}

func (m *Client) Send(to []string, subject, body string) error {
	return m.SendContext(context.Background(), to, subject, body)
}

func (m *Client) SendContext(ctx context.Context, to []string, subject, body string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(m.conf.Host, strconv.Itoa(m.conf.Port)))
	if err != nil {
		return err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		if err := conn.SetDeadline(deadline); err != nil {
			return err
		}
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	client, err := m.handshake(conn)
	if err != nil {
		return err
	}
	defer client.Close()
	msg := gomail.NewMessage()
	msg.SetAddressHeader("From", m.conf.From, m.conf.SiteName)
	if m.conf.ReplyTo != "" {
		msg.SetHeader("Reply-To", m.conf.ReplyTo)
	}
	msg.SetHeader("To", to...)
	msg.SetHeader("Subject", subject)
	msg.SetBody("text/html", body)
	if err := gomail.Send(smtpTransfer{client: client}, msg); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	_ = client.Quit()
	return nil
}

func (m *Client) handshake(conn net.Conn) (*smtp.Client, error) {
	if m.dailer.SSL {
		conn = tls.Client(conn, m.dailer.TLSConfig)
	}
	client, err := smtp.NewClient(conn, m.conf.Host)
	if err != nil {
		return nil, err
	}
	if !m.dailer.SSL {
		if supported, _ := client.Extension("STARTTLS"); supported {
			if err := client.StartTLS(m.dailer.TLSConfig); err != nil {
				_ = client.Close()
				return nil, err
			}
		} else if m.conf.SSL {
			_ = client.Close()
			return nil, fmt.Errorf("SMTP relay does not support required STARTTLS")
		}
	}
	if supported, mechanisms := client.Extension("AUTH"); m.conf.User != "" && supported {
		auth := smtp.PlainAuth("", m.conf.User, m.conf.Pass, m.conf.Host)
		if strings.Contains(mechanisms, "CRAM-MD5") {
			auth = smtp.CRAMMD5Auth(m.conf.User, m.conf.Pass)
		} else if strings.Contains(mechanisms, "LOGIN") && !strings.Contains(mechanisms, "PLAIN") {
			auth = &loginAuth{host: m.conf.Host, username: m.conf.User, password: m.conf.Pass}
		}
		if err := client.Auth(auth); err != nil {
			_ = client.Close()
			return nil, err
		}
	}
	return client, nil
}

type smtpTransfer struct{ client *smtp.Client }

func (transfer smtpTransfer) Send(from string, to []string, message io.WriterTo) error {
	if err := transfer.client.Mail(from); err != nil {
		return err
	}
	for _, address := range to {
		if err := transfer.client.Rcpt(address); err != nil {
			return err
		}
	}
	writer, err := transfer.client.Data()
	if err != nil {
		return err
	}
	if _, err := message.WriteTo(writer); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

type loginAuth struct{ host, username, password string }

func (auth *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !server.TLS || server.Name != auth.host {
		return "", nil, fmt.Errorf("SMTP LOGIN requires TLS to the configured relay")
	}
	return "LOGIN", nil, nil
}

func (auth *loginAuth) Next(challenge []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	switch string(challenge) {
	case "Username:":
		return []byte(auth.username), nil
	case "Password:":
		return []byte(auth.password), nil
	default:
		return nil, fmt.Errorf("unexpected SMTP LOGIN challenge")
	}
}
