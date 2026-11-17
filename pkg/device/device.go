package device

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"

	"github.com/gorilla/websocket"
)

type Operator int

const (
	MaxDevices Operator = iota
	Admin
	SubscribeUpdate = "subscribe_update"
)

// Device represents a device structure
type Device struct {
	Session      string
	DeviceID     string
	Conn         *websocket.Conn
	CreatedAt    time.Time
	LastPingTime time.Time
	mu           sync.Mutex
	send         chan []byte
	closing      chan struct{}
	closeOnce    sync.Once
	writerDone   chan struct{}
}

const (
	writeTimeout   = 10 * time.Second
	maxMessageSize = 4 << 10
	sendQueueSize  = 32
	messageWorkers = 64
)

var ErrSendQueueFull = errors.New("device send queue is full")
var ErrDeviceClosed = errors.New("device socket is closed")

func (device *Device) enqueue(message string) error {
	device.mu.Lock()
	defer device.mu.Unlock()
	select {
	case <-device.closing:
		return ErrDeviceClosed
	default:
	}
	select {
	case device.send <- []byte(message):
		return nil
	default:
		return ErrSendQueueFull
	}
}

func (device *Device) close() {
	device.mu.Lock()
	defer device.mu.Unlock()
	device.closeOnce.Do(func() { close(device.closing) })
}

func (device *Device) touch() {
	device.mu.Lock()
	device.LastPingTime = time.Now()
	device.mu.Unlock()
}

func (device *Device) lastPing() time.Time {
	device.mu.Lock()
	defer device.mu.Unlock()
	return device.LastPingTime
}

func (device *Device) writeMessages() {
	defer close(device.writerDone)
	defer device.Conn.Close()
	write := func(message []byte, deadline time.Time) error {
		if err := device.Conn.SetWriteDeadline(deadline); err != nil {
			return err
		}
		return device.Conn.WriteMessage(websocket.TextMessage, message)
	}
	for {
		select {
		case <-device.closing:
			deadline := time.Now().Add(writeTimeout)
			for {
				select {
				case message := <-device.send:
					if err := write(message, deadline); err != nil {
						return
					}
				default:
					return
				}
			}
		case message := <-device.send:
			if err := write(message, time.Now().Add(writeTimeout)); err != nil {
				return
			}
		}
	}
}

// DeviceManager manages devices
type DeviceManager struct {
	userDevices      sync.Map // userID -> []*Device
	totalOnline      int32    // total online devices
	userMutexes      sync.Map // userID level locks
	heartbeatTimeout int      // heartbeat timeout (seconds)
	checkInterval    int      // heartbeat check interval (seconds)
	stop             chan struct{}
	done             chan struct{}
	stopOnce         sync.Once
	closed           atomic.Bool
	upgrader         websocket.Upgrader
	messageSlots     chan struct{}
	lifecycleMu      sync.Mutex
	workers          sync.WaitGroup
	callbacks        sync.WaitGroup

	// event callbacks
	OnDeviceOnline  func(userID int64, deviceID, session string)
	OnDeviceOffline func(userID int64, deviceID, session string, createAt time.Time)
	OnDeviceKicked  func(userID int64, deviceID, session string, operator Operator)
	OnMessage       func(userID int64, deviceID, session string, message string)
}

// Get user-level mutex
func (dm *DeviceManager) getUserMutex(userID int64) *sync.Mutex {
	mu, _ := dm.userMutexes.LoadOrStore(userID, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// Listen to WebSocket data
func (dm *DeviceManager) listenToDevice(userID int64, device *Device) {
	defer func() {
		dm.removeDevice(userID, device)
		device.close()
		<-device.writerDone
		dm.workers.Done()
	}()
	device.Conn.SetReadLimit(maxMessageSize)
	device.Conn.SetPingHandler(func(message string) error {
		device.touch()
		if err := device.Conn.SetReadDeadline(time.Now().Add(time.Duration(dm.heartbeatTimeout) * time.Second)); err != nil {
			return err
		}
		return device.Conn.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(writeTimeout))
	})
	for {
		if err := device.Conn.SetReadDeadline(time.Now().Add(time.Duration(dm.heartbeatTimeout) * time.Second)); err != nil {
			return
		}
		_, msg, err := device.Conn.ReadMessage()
		if err != nil {
			return
		}
		device.touch()
		message := string(msg)
		if message == "ping" || message == "heartbeat" {
			if err := device.enqueue("ping"); err != nil {
				return
			}
			continue
		}
		if dm.OnMessage != nil {
			dm.callback(func() { dm.OnMessage(userID, device.DeviceID, device.Session, message) })
		}
	}
}

func (dm *DeviceManager) callback(callback func()) {
	select {
	case dm.messageSlots <- struct{}{}:
	case <-dm.stop:
		return
	}
	dm.lifecycleMu.Lock()
	if dm.closed.Load() {
		dm.lifecycleMu.Unlock()
		<-dm.messageSlots
		return
	}
	dm.callbacks.Add(1)
	dm.lifecycleMu.Unlock()
	go func() {
		defer dm.callbacks.Done()
		defer func() { <-dm.messageSlots }()
		callback()
	}()
}

func (dm *DeviceManager) snapshot(userID int64) []*Device {
	mu := dm.getUserMutex(userID)
	mu.Lock()
	defer mu.Unlock()
	if value, ok := dm.userDevices.Load(userID); ok {
		return append([]*Device(nil), value.([]*Device)...)
	}
	return nil
}

// UpdateHeartbeat updates device heartbeat
func (dm *DeviceManager) UpdateHeartbeat(userID int64, deviceID string) {
	for _, device := range dm.snapshot(userID) {
		if device.DeviceID == deviceID {
			device.touch()
			_ = device.enqueue("ping")
			return
		}
	}
}

// AddDevice **Add: Device connects WebSocket and is added to the manager**
func (dm *DeviceManager) AddDevice(w http.ResponseWriter, r *http.Request, session string, userID int64, deviceID string, maxDevices int) {
	if dm.closed.Load() {
		http.Error(w, "device service unavailable", http.StatusServiceUnavailable)
		return
	}
	// **Upgrade WebSocket connection**
	conn, err := dm.upgrader.Upgrade(w, r, nil)
	if err != nil {
		zap.S().Infof("WebSocket upgrade failed: %v", err)
		return
	}

	device := dm.register(conn, session, userID, deviceID, maxDevices)
	if device != nil {
		go dm.listenToDevice(userID, device)
	}
}

func (dm *DeviceManager) register(conn *websocket.Conn, session string, userID int64, deviceID string, maxDevices int) *Device {
	dm.lifecycleMu.Lock()
	if dm.closed.Load() {
		dm.lifecycleMu.Unlock()
		_ = conn.Close()
		return nil
	}
	dm.workers.Add(1)
	dm.lifecycleMu.Unlock()
	if maxDevices < 1 {
		maxDevices = 99
	}
	now := time.Now()
	device := &Device{Session: session, DeviceID: deviceID, Conn: conn, CreatedAt: now, LastPingTime: now,
		send: make(chan []byte, sendQueueSize), closing: make(chan struct{}), writerDone: make(chan struct{})}
	go device.writeMessages()
	mu := dm.getUserMutex(userID)
	mu.Lock()
	var devices []*Device
	var replaced, kicked *Device
	if value, ok := dm.userDevices.Load(userID); ok {
		for _, current := range value.([]*Device) {
			if current.DeviceID == deviceID {
				replaced = current
			} else {
				devices = append(devices, current)
			}
		}
	}
	if replaced == nil && len(devices) >= maxDevices {
		kicked = devices[0]
	}
	devices = append(devices, device)
	dm.userDevices.Store(userID, devices)
	if replaced == nil {
		atomic.AddInt32(&dm.totalOnline, 1)
	}
	if dm.closed.Load() {
		device.close()
		_ = conn.Close()
	}
	mu.Unlock()
	if replaced != nil {
		replaced.close()
	}
	if kicked != nil {
		if dm.OnDeviceKicked != nil {
			dm.OnDeviceKicked(userID, kicked.DeviceID, kicked.Session, MaxDevices)
		}
		dm.removeDevice(userID, kicked)
	}
	if dm.OnDeviceOnline != nil {
		dm.callback(func() { dm.OnDeviceOnline(userID, deviceID, session) })
	}
	return device
}

func (dm *DeviceManager) removeDevice(userID int64, device *Device) {
	mu := dm.getUserMutex(userID)
	mu.Lock()
	removed := false
	if value, ok := dm.userDevices.Load(userID); ok {
		devices := value.([]*Device)
		for index, current := range devices {
			if current == device {
				devices = append(devices[:index], devices[index+1:]...)
				atomic.AddInt32(&dm.totalOnline, -1)
				removed = true
				break
			}
		}
		if len(devices) == 0 {
			dm.userDevices.Delete(userID)
		} else {
			dm.userDevices.Store(userID, devices)
		}
	}
	mu.Unlock()
	device.close()
	if removed && dm.OnDeviceOffline != nil {
		dm.callback(func() { dm.OnDeviceOffline(userID, device.DeviceID, device.Session, device.CreatedAt) })
	}
}

// KickDevice kicks a device (supports individual device or entire user)
func (dm *DeviceManager) KickDevice(userID int64, deviceID string) {
	for _, device := range dm.snapshot(userID) {
		if deviceID == "" || device.DeviceID == deviceID {
			if dm.OnDeviceKicked != nil {
				dm.OnDeviceKicked(userID, device.DeviceID, device.Session, Admin)
			}
			dm.removeDevice(userID, device)
		}
	}
}

// StartHeartbeatCheck periodically checks for heartbeat timeout devices
func (dm *DeviceManager) StartHeartbeatCheck() {
	defer close(dm.done)
	ticker := time.NewTicker(time.Duration(dm.checkInterval) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-dm.stop:
			return
		case <-ticker.C:
		}
		now := time.Now()

		dm.userDevices.Range(func(userID, _ interface{}) bool {
			uid := userID.(int64)
			for _, device := range dm.snapshot(uid) {
				if now.Sub(device.lastPing()) > time.Duration(dm.heartbeatTimeout)*time.Second {
					dm.removeDevice(uid, device)
				}
			}
			return true
		})
		//zap.S().Infof("Total online devices: %d\n", dm.totalOnline)
	}
}

// NewDeviceManager creates a new device manager
func NewDeviceManager(heartbeatTimeout, checkInterval int, allowedOrigins ...[]string) *DeviceManager {
	if heartbeatTimeout <= 0 {
		heartbeatTimeout = 30
	}
	if checkInterval <= 0 {
		checkInterval = 30
	}
	origins := make(map[string]struct{})
	if len(allowedOrigins) > 0 {
		for _, origin := range allowedOrigins[0] {
			origins[strings.ToLower(strings.TrimSuffix(strings.TrimSpace(origin), "/"))] = struct{}{}
		}
	}
	dm := &DeviceManager{
		heartbeatTimeout: heartbeatTimeout,
		checkInterval:    checkInterval,
		stop:             make(chan struct{}),
		done:             make(chan struct{}),
		messageSlots:     make(chan struct{}, messageWorkers),
		upgrader: websocket.Upgrader{HandshakeTimeout: writeTimeout, CheckOrigin: func(request *http.Request) bool {
			origin := request.Header.Get("Origin")
			if origin == "" || len(origins) == 0 {
				return true
			}
			_, allowed := origins[strings.ToLower(strings.TrimSuffix(strings.TrimSpace(origin), "/"))]
			return allowed
		}},
	}
	go dm.StartHeartbeatCheck()
	return dm
}

// SendToDevice sends a message to a specific device
func (dm *DeviceManager) SendToDevice(userID int64, deviceID string, message string) error {
	found := false
	var failures []error
	for _, device := range dm.snapshot(userID) {
		if deviceID == "" || device.DeviceID == deviceID {
			found = true
			if err := device.enqueue(message); err != nil {
				failures = append(failures, err)
			}
		}
	}
	if found {
		return errors.Join(failures...)
	}
	return fmt.Errorf("device %s (User %d) is offline", deviceID, userID)
}

// Broadcast sends a message to all devices
func (dm *DeviceManager) Broadcast(message string) {
	dm.userDevices.Range(func(userID, _ any) bool {
		_ = dm.SendToDevice(userID.(int64), "", message)
		return true
	})
}

// Gracefully shut down all WebSocket connections
func (dm *DeviceManager) Shutdown(ctx context.Context) {
	<-ctx.Done()
	dm.Close()
}

func (dm *DeviceManager) Close() {
	dm.stopOnce.Do(func() {
		dm.lifecycleMu.Lock()
		dm.closed.Store(true)
		close(dm.stop)
		dm.lifecycleMu.Unlock()
		<-dm.done
		dm.userDevices.Range(func(userID, _ any) bool {
			mu := dm.getUserMutex(userID.(int64))
			mu.Lock()
			defer mu.Unlock()
			value, ok := dm.userDevices.LoadAndDelete(userID)
			if ok {
				for _, device := range value.([]*Device) {
					device.close()
					_ = device.Conn.Close()
					atomic.AddInt32(&dm.totalOnline, -1)
				}
			}
			return true
		})
		dm.workers.Wait()
		dm.callbacks.Wait()
	})
}
