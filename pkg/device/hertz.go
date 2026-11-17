package device

import (
	"bufio"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/gorilla/websocket"
)

type HandshakeError struct {
	Status int
	Reason string
}

func (failure *HandshakeError) Error() string { return failure.Reason }

func (dm *DeviceManager) UpgradeHertz(request *app.RequestContext, session string, userID int64, deviceID string, maxDevices int) error {
	if dm.closed.Load() {
		return &HandshakeError{Status: http.StatusServiceUnavailable, Reason: "device service unavailable"}
	}
	compat, err := http.NewRequestWithContext(context.Background(), string(request.Method()), request.URI().String(), http.NoBody)
	if err != nil {
		return &HandshakeError{Status: http.StatusBadRequest, Reason: "malformed request"}
	}
	request.Request.Header.VisitAll(func(key, value []byte) { compat.Header.Add(string(key), string(value)) })
	if compat.Method != http.MethodGet || !websocket.IsWebSocketUpgrade(compat) || compat.Header.Get("Sec-WebSocket-Version") != "13" {
		return &HandshakeError{Status: http.StatusBadRequest, Reason: "invalid websocket handshake"}
	}
	challenge := compat.Header.Get("Sec-WebSocket-Key")
	decoded, err := base64.StdEncoding.DecodeString(challenge)
	if err != nil || len(decoded) != 16 {
		return &HandshakeError{Status: http.StatusBadRequest, Reason: "invalid websocket key"}
	}
	if !dm.upgrader.CheckOrigin(compat) {
		return &HandshakeError{Status: http.StatusForbidden, Reason: "origin not allowed"}
	}
	digest := sha1.Sum([]byte(challenge + "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"))
	request.SetStatusCode(http.StatusSwitchingProtocols)
	request.Response.Header.Set("Upgrade", "websocket")
	request.Response.Header.Set("Connection", "Upgrade")
	request.Response.Header.Set("Sec-WebSocket-Accept", base64.StdEncoding.EncodeToString(digest[:]))
	raw := request.GetConn()
	request.Hijack(func(connection network.Conn) {
		bridge := &hertzSocketConn{Conn: connection, raw: raw}
		writer := &hertzSocketWriter{connection: bridge, header: make(http.Header)}
		socket, err := dm.upgrader.Upgrade(writer, compat, nil)
		if err != nil {
			_ = bridge.Close()
			return
		}
		device := dm.register(socket, session, userID, deviceID, maxDevices)
		if device != nil {
			dm.listenToDevice(userID, device)
		}
	})
	return nil
}

type hertzSocketConn struct {
	network.Conn
	raw        network.Conn
	handshake  atomic.Bool
	closeOnce  sync.Once
	closeError error
}

func (connection *hertzSocketConn) Write(message []byte) (int, error) {
	if connection.handshake.CompareAndSwap(false, true) {
		return len(message), nil
	}
	return connection.Conn.Write(message)
}

func (connection *hertzSocketConn) Close() error {
	connection.closeOnce.Do(func() { connection.closeError = connection.raw.Close() })
	return connection.closeError
}

type hertzSocketWriter struct {
	connection net.Conn
	header     http.Header
}

func (writer *hertzSocketWriter) Header() http.Header               { return writer.header }
func (writer *hertzSocketWriter) Write(message []byte) (int, error) { return len(message), nil }
func (writer *hertzSocketWriter) WriteHeader(int)                   {}
func (writer *hertzSocketWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if writer.connection == nil {
		return nil, nil, fmt.Errorf("websocket connection unavailable")
	}
	return writer.connection, bufio.NewReadWriter(bufio.NewReader(writer.connection), bufio.NewWriter(writer.connection)), nil
}
