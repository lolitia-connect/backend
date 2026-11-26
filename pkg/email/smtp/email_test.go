package smtp

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"mime/quotedprintable"
	"net"
	"net/http/httptest"
	"net/mail"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTLSDefaultsAndExplicitOverrides(t *testing.T) {
	client := NewClient(&Config{Host: "smtp.example.com", Port: 587, SSL: true})
	if client.dailer.TLSConfig.InsecureSkipVerify || client.dailer.SSL {
		t.Fatal("STARTTLS client skips verification or uses implicit TLS")
	}
	for _, config := range []Config{{Port: 465}, {Port: 2465, ImplicitTLS: true}} {
		if !NewClient(&config).dailer.SSL {
			t.Fatal("implicit TLS not enabled")
		}
	}
	if !NewClient(&Config{InsecureSkipVerify: true}).dailer.TLSConfig.InsecureSkipVerify {
		t.Fatal("explicit certificate override ignored")
	}
}

func TestRequiredTLSRefusesPlaintextRelayBeforeAuthentication(t *testing.T) {
	clientConn, relayConn := net.Pipe()
	defer clientConn.Close()
	defer relayConn.Close()
	_ = clientConn.SetDeadline(time.Now().Add(3 * time.Second))
	_ = relayConn.SetDeadline(time.Now().Add(3 * time.Second))
	commands := make(chan string, 1)
	go func() {
		_, _ = fmt.Fprint(relayConn, "220 relay ESMTP\r\n")
		reader := bufio.NewReader(relayConn)
		line, _ := reader.ReadString('\n')
		_, _ = fmt.Fprint(relayConn, "250-relay\r\n250 AUTH PLAIN\r\n")
		following, _ := reader.ReadString('\n')
		commands <- line + following
	}()
	client := NewClient(&Config{Host: "relay", Port: 587, SSL: true, User: "user", Pass: "secret"})
	if _, err := client.handshake(clientConn); err == nil {
		t.Fatal("plaintext relay accepted")
	}
	if sent := <-commands; strings.Contains(sent, "AUTH") || strings.Contains(sent, "MAIL") {
		t.Fatalf("credentials or mail sent without required encryption: %s", sent)
	}
}

func TestImplicitTLSRelayDeliversMailAfterLoginRecovers(t *testing.T) {
	certs := httptest.NewTLSServer(nil)
	serverTLS := certs.TLS.Clone()
	roots := x509.NewCertPool()
	roots.AddCert(certs.Certificate())
	certs.Close()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	conf := Config{Host: "127.0.0.1", Port: listener.Addr().(*net.TCPAddr).Port, ImplicitTLS: true,
		User: "relay-test", Pass: "fixture-password", From: "notices@example.test", ReplyTo: "help@example.test", SiteName: "Member portal"}
	client := NewClient(&conf)
	client.dailer.TLSConfig.RootCAs = roots
	const recipient = "reader@example.test"
	const subject = "May receipt"
	const body = "<p>Invoice 72 has been paid.</p>"
	for _, accept := range []bool{false, true} {
		delivered := make(chan error, 1)
		go func() { delivered <- client.Send([]string{recipient}, subject, body) }()
		deadline := time.Now().Add(8 * time.Second)
		require.NoError(t, listener.SetDeadline(deadline))
		conn, err := listener.Accept()
		require.NoError(t, err)
		t.Cleanup(func() { _ = conn.Close() })
		require.NoError(t, conn.SetDeadline(deadline))
		wire := textproto.NewConn(tls.Server(conn, serverTLS))
		send := func(s string) { t.Helper(); require.NoError(t, wire.PrintfLine("%s", s)) }
		read := func() string {
			t.Helper()
			line, err := wire.ReadLine()
			require.NoError(t, err)
			return line
		}
		send("220 relay ESMTP")
		require.True(t, strings.HasPrefix(read(), "EHLO "))
		send("250-relay")
		send("250 AUTH LOGIN")
		require.Equal(t, "AUTH LOGIN", strings.TrimSpace(read()))
		for _, challenge := range []struct{ prompt, value string }{{"Username:", conf.User}, {"Password:", conf.Pass}} {
			send("334 " + base64.StdEncoding.EncodeToString([]byte(challenge.prompt)))
			require.Equal(t, base64.StdEncoding.EncodeToString([]byte(challenge.value)), read())
		}
		if !accept {
			send("535 Relay account unavailable")
			line := read()
			if line == "*" {
				send("501 Authentication cancelled")
				line = read()
			}
			require.Equal(t, "QUIT", line)
			send("221 Bye")
			require.ErrorContains(t, <-delivered, "535")
			continue
		}
		send("235 Authenticated")
		require.Equal(t, "MAIL FROM:<"+conf.From+">", read())
		send("250 OK")
		require.Equal(t, "RCPT TO:<"+recipient+">", read())
		send("250 OK")
		require.Equal(t, "DATA", read())
		send("354 Send message")
		data, err := wire.ReadDotBytes()
		require.NoError(t, err)
		send("250 Accepted")
		require.Equal(t, "QUIT", read())
		send("221 Bye")
		require.NoError(t, <-delivered)
		message, err := mail.ReadMessage(bytes.NewReader(data))
		require.NoError(t, err)
		require.Equal(t, subject, message.Header.Get("Subject"))
		require.Equal(t, conf.ReplyTo, message.Header.Get("Reply-To"))
		content, err := io.ReadAll(quotedprintable.NewReader(message.Body))
		require.NoError(t, err)
		require.Equal(t, body, strings.TrimSpace(string(content)))
	}
}
