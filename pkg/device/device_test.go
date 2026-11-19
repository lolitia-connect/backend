package device

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

func TestDevice(t *testing.T) {
	t.Skip("skip test")
	/*	deviceManager := NewDeviceManager(10, 3)

		deviceManager.OnDeviceOnline = func(userID int64, deviceID, session string) {
			fmt.Printf("✅ 设备 %s (用户 %d) 上线\n", deviceID, userID)
		}

		deviceManager.OnDeviceOffline = func(userID int64, deviceID, session string) {
			fmt.Printf("❌ 设备 %s (用户 %d) 下线\n", deviceID, userID)
		}

		deviceManager.OnDeviceKicked = func(userID int64, deviceID, session string, operator Operator) {
			fmt.Printf("⚠️ 设备 %s (用户 %d) 被踢下线\n", deviceID, userID)
		}
		deviceManager.OnMessage = func(userID int64, deviceID, session string, message string) {
			log.Printf("✅收到消息:  设备 %s (用户 %d) 内容: %s,sesion: %s\n", deviceID, userID, message, session)
		}
		engine := hertzx.Default()
		engine.GET("/ws/:userid/:device_number", func(c *hertzx.Context) {
			//根据Authorization获取session
			authorization := c.GetHeader("Authorization")
			userid, err := strconv.ParseInt(c.Param("userid"), 10, 64)
			if err != nil {
				t.Errorf("get user id err:%v", err)
				return
			}
			deviceNumber := c.Param("device_number")
			deviceManager.AddDevice(c, authorization, userid, deviceNumber, 3)
			return
		})
		go func() {
			err := http.ListenAndServe(":8081", engine)
			if err != nil {
				t.Fatalf("engine start failed: %v", err)
			}
		}()
	*/
	h := http.Header{}
	h.Add("Authorization", "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJTZXNzaW9uSWQiOiIwMTk0Y2ZiNy1hYjY0LTdjYjMtODUzYi03ZGU5YTAzNWRlZTgiLCJVc2VySWQiOjI5LCJleHAiOjE3MzkyNTY1MDgsImlhdCI6MTczODY1MTcwOH0.BGKT5-hongJPZrA_yAb6cf6go5iDR8T9uu1ZxUg8HDw")

	mutex := sync.Mutex{}
	serverURL := fmt.Sprintf("ws://localhost:8080/v1/app/ws/%d/%s", 29, "15502502051") // 假设 userID 为 1001，设备ID 为 deviceA

	// 建立 WebSocket 连接
	conn, resp, err := websocket.DefaultDialer.Dial(serverURL, h)
	if err != nil {
		all, err := io.ReadAll(resp.Body)
		t.Fatalf("websocket dial failed: %v:%s", err, string(all))
	}
	// 启动一个 goroutine 来读取服务器消息
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				if errors.Is(err, net.ErrClosed) || strings.Contains(err.Error(), "use of closed network connection") {
					log.Println("连接已关闭")
					return
				}
				log.Printf("接收消息失败: %v", err)
				return
			}
			fmt.Printf("收到来自服务器的消息: %s\n", msg)
		}
	}()

	//发送心跳
	go func() {
		ticker := time.NewTicker(time.Second * 5)
		defer ticker.Stop()

		for range ticker.C {
			mutex.Lock()
			err := conn.WriteMessage(websocket.TextMessage, []byte("ping"))
			mutex.Unlock()

			if err != nil {
				if strings.Contains(err.Error(), "use of closed network connection") {
					log.Println("连接已关闭")
					return
				}
				t.Errorf("websocket 写入失败: %v", err)
				return
			}
		}
	}()

	updateSubscribe, _ := json.Marshal(map[string]interface{}{
		"method": "test_method",
	})

	//发送一条消息
	mutex.Lock()
	err = conn.WriteMessage(websocket.TextMessage, updateSubscribe)
	mutex.Unlock()
	if err != nil {
		t.Errorf("websocket write failed: %v", err)
	}

	time.Sleep(time.Second * 20)
	conn.Close()
	time.Sleep(time.Second * 5)

}
