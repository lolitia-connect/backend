package smtp

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
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

func TestEmailSend(t *testing.T) {
	t.Skipf("Skip TestEmailSend")
	config := &Config{
		Host:     "smtp.mail.me.com",
		Port:     587,
		User:     "support@ppanel.dev",
		Pass:     "password",
		From:     "support@ppanel.dev",
		SSL:      true,
		SiteName: "",
	}
	address := []string{"tension@sparkdance.dev"}
	subject := "test"
	body := "test"
	email := NewClient(config)
	err := email.Send(address, subject, body)
	if err != nil {
		t.Errorf("send email error: %v", err)
	}
}
