package device

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestDeviceManagerRoutesMessagesAndShutdown(t *testing.T) {
	manager := NewDeviceManager(30, 30)
	message := make(chan string, 1)
	manager.OnMessage = func(_ int64, deviceID, _ string, body string) {
		message <- deviceID + ":" + body
	}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		manager.AddDevice(response, request, "session", 12, "device", 2)
	}))
	t.Cleanup(server.Close)
	t.Cleanup(manager.Close)
	connection, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if err := connection.WriteMessage(websocket.TextMessage, []byte("client-message")); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-message:
		if got != "device:client-message" {
			t.Fatalf("message callback = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("device message callback did not run")
	}
	if err := manager.SendToDevice(12, "device", "direct"); err != nil {
		t.Fatal(err)
	}
	_ = connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, body, err := connection.ReadMessage(); err != nil || string(body) != "direct" {
		t.Fatalf("direct device message = %q, %v", body, err)
	}
	manager.Broadcast("broadcast")
	if _, body, err := connection.ReadMessage(); err != nil || string(body) != "broadcast" {
		t.Fatalf("broadcast device message = %q, %v", body, err)
	}
	if err := manager.SendToDevice(12, "missing", "offline"); err == nil {
		t.Fatal("offline device accepted a message")
	}
	manager.KickDevice(12, "device")
	if _, _, err := connection.ReadMessage(); err == nil {
		t.Fatal("kicked device remained connected")
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { manager.Shutdown(cancelCtx); close(stopped) }()
	cancel()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("device shutdown did not complete")
	}
}
