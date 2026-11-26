package device

import (
	"context"

	"fmt"

	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/network/standard"
	"github.com/gorilla/websocket"
)

func TestHertzSocketHandoffAndKick(t *testing.T) {
	manager := NewDeviceManager(30, 30, []string{"https://panel.example"})
	defer manager.Close()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	engine := server.New(server.WithListener(listener), server.WithTransport(standard.NewTransporter), server.WithDisablePrintRoute(true))
	engine.GET("/socket", func(_ context.Context, request *app.RequestContext) {
		if err := manager.UpgradeHertz(request, "session", 7, "device", 3); err != nil {
			request.String(http.StatusBadRequest, "%s", err)
		}
	})
	result := make(chan error, 1)
	go func() { result <- engine.Run() }()
	defer func() {
		manager.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := engine.Shutdown(ctx); err != nil {
			t.Error(err)
		}
		select {
		case err := <-result:
			if err != nil {
				t.Error(err)
			}
		case <-ctx.Done():
			t.Error("Hertz listener did not stop")
		}
	}()
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	connection, _, err := dialer.Dial("ws://"+listener.Addr().String()+"/socket", http.Header{"Origin": {"https://panel.example"}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, message, err := connection.ReadMessage(); err != nil || string(message) != "ping" {
		t.Fatalf("Hertz handoff corrupted websocket data: %q, %v", message, err)
	}
	manager.KickDevice(7, "device")
	if _, _, err := connection.ReadMessage(); err == nil {
		t.Fatal("kick did not close the raw Hertz connection")
	}
}

func TestReconnectAndKickPreserveConnectionIdentity(t *testing.T) {
	manager := NewDeviceManager(30, 30)
	connected := make(chan string, 4)
	manager.OnDeviceOnline = func(_ int64, identifier, _ string) { connected <- identifier }
	kickErrors := make(chan error, 1)
	manager.OnDeviceKicked = func(userID int64, identifier, _ string, _ Operator) {
		kickErrors <- manager.SendToDevice(userID, identifier, "kicked")
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		manager.AddDevice(response, request, "session", 7, request.URL.Query().Get("id"), 1)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(manager.Close)
	dial := func(identifier string) *websocket.Conn {
		t.Helper()
		connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http")+"/?id="+identifier, nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = connection.Close() })
		select {
		case <-connected:
		case <-time.After(2 * time.Second):
			t.Fatal("device was not registered")
		}
		return connection
	}
	previous := dial("same")
	current := dial("same")
	_ = previous.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := previous.ReadMessage(); err == nil {
		t.Fatal("replaced connection remained open")
	}
	if err := current.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = current.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, reply, err := current.ReadMessage(); err != nil || string(reply) != "ping" {
		t.Fatalf("replacement lost after old disconnect: %s, %v", reply, err)
	}
	if atomic.LoadInt32(&manager.totalOnline) != 1 {
		t.Fatal("reconnect changed the online count")
	}
	other := dial("other")
	if err := <-kickErrors; err != nil {
		t.Fatalf("kick notification: %v", err)
	}
	if _, reply, err := current.ReadMessage(); err != nil || string(reply) != "kicked" {
		t.Fatalf("kick notification was lost: %s, %v", reply, err)
	}
	if _, _, err := current.ReadMessage(); err == nil {
		t.Fatal("kicked connection remained open")
	}
	if err := other.WriteMessage(websocket.TextMessage, []byte("heartbeat")); err != nil {
		t.Fatal(err)
	}
	_ = other.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, reply, err := other.ReadMessage(); err != nil || string(reply) != "ping" {
		t.Fatalf("new device blocked by kick: %s, %v", reply, err)
	}
}

func TestSocketOriginAndReadLimit(t *testing.T) {
	manager := NewDeviceManager(30, 30, []string{"https://Panel.Example/"})
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		manager.AddDevice(response, request, "session", 7, "device", 3)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(manager.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	connection, response, err := websocket.DefaultDialer.Dial(url, http.Header{"Origin": {"https://other.example"}})
	if err == nil {
		_ = connection.Close()
		t.Fatal("foreign origin accepted")
	}
	if response == nil || response.StatusCode != http.StatusForbidden {
		t.Fatalf("origin rejection = %+v, %v", response, err)
	}
	_ = response.Body.Close()
	connection, _, err = websocket.DefaultDialer.Dial(url, http.Header{"Origin": {"https://panel.example"}})
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", maxMessageSize+1))); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := connection.ReadMessage(); !websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		t.Fatalf("oversized frame = %v", err)
	}
}

func TestDeviceSendQueueIsBounded(t *testing.T) {
	device := &Device{send: make(chan []byte, sendQueueSize), closing: make(chan struct{})}
	for index := 0; index < sendQueueSize; index++ {
		if err := device.enqueue("message"); err != nil {
			t.Fatal(err)
		}
	}
	if err := device.enqueue("overflow"); !errors.Is(err, ErrSendQueueFull) {
		t.Fatalf("queue overflow = %v", err)
	}
	device.close()
	if err := device.enqueue("closed"); !errors.Is(err, ErrDeviceClosed) {
		t.Fatalf("closed send = %v", err)
	}
}

func TestCloseStopsHeartbeatAndIsIdempotent(t *testing.T) {
	manager := NewDeviceManager(30, 30)
	manager.Close()
	manager.Close()
	select {
	case <-manager.done:
	default:
		t.Fatal("heartbeat worker still running after close")
	}
	if !manager.closed.Load() || atomic.LoadInt32(&manager.totalOnline) != 0 {
		t.Fatal("device manager did not finish closing")
	}
}

func TestShutdownWaitsForAcceptedDeviceMessage(t *testing.T) {
	manager := NewDeviceManager(30, 30)
	entered := make(chan string, 1)
	release := make(chan struct{})
	finish := sync.OnceFunc(func() { close(release) })
	manager.OnMessage = func(userID int64, deviceID, session, message string) {
		entered <- fmt.Sprintf("%d/%s/%s: %s", userID, deviceID, session, message)
		<-release
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		manager.AddDevice(w, r, "billing-session", 42, "tablet", 2)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(manager.Close)
	t.Cleanup(finish)
	dialer := websocket.Dialer{HandshakeTimeout: 2 * time.Second}
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	conn, _, err := dialer.DialContext(t.Context(), url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.WriteMessage(websocket.TextMessage, []byte("subscription received")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-entered:
		if got != "42/tablet/billing-session: subscription received" {
			t.Fatalf("last message delivered as %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("device message did not reach its callback")
	}
	stopped := make(chan struct{})
	go func() { manager.Close(); close(stopped) }()
	if err := conn.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := conn.ReadMessage(); err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected server to disconnect the active device, got %v", err)
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatalf("active device timed out before server closed it: %v", err)
	}
	if next, response, err := dialer.DialContext(t.Context(), url, nil); err == nil {
		_ = next.Close()
		t.Fatal("a new device connected during shutdown")
	} else if response == nil {
		t.Fatalf("missing handshake response: %v", err)
	} else {
		_ = response.Body.Close()
		if response.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("new device received status %d during shutdown", response.StatusCode)
		}
	}
	select {
	case <-stopped:
		t.Fatal("shutdown completed before the accepted message was handled")
	default:
	}
	finish()
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown did not finish after the callback returned")
	}
}
