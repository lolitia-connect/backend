package logging

import (
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const (
	accessFilename = "access.log"
	fileMode       = "file"
	volumeMode     = "volume"
	plainEncoding  = "plain"
	sizeRotation   = "size"
	megabyte       = 1 << 20
)

var (
	ErrLogPathNotSet        = errors.New("log path must be set")
	ErrLogServiceNameNotSet = errors.New("log service name must be set")
)

// LogConf is the logger configuration loaded from config.yaml.
type LogConf struct {
	ServiceName         string `yaml:"ServiceName" default:"PPanel"`
	Mode                string `yaml:"Mode" default:"file"`
	Encoding            string `yaml:"Encoding" default:"json"`
	TimeFormat          string `yaml:"TimeFormat" default:"2006-01-02 15:04:05.000"`
	Path                string `yaml:"Path" default:"logs"`
	FilePath            string `yaml:"FilePath"`
	Level               string `yaml:"Level" default:"info"`
	MaxContentLength    uint32 `yaml:"MaxContentLength" default:"0"`
	Compress            bool   `yaml:"Compress" default:"false"`
	Stat                bool   `yaml:"Stat" default:"true"`
	KeepDays            int    `yaml:"KeepDays" default:"0"`
	MaxAge              int    `yaml:"MaxAge" default:"0"`
	StackCooldownMillis int    `yaml:"StackCooldownMillis" default:"100"`
	MaxBackups          int    `yaml:"MaxBackups" default:"0"`
	MaxBackup           int    `yaml:"MaxBackup" default:"0"`
	MaxSize             int    `yaml:"MaxSize" default:"0"`
	Rotation            string `yaml:"Rotation" default:"daily"`
	FileTimeFormat      string `yaml:"FileTimeFormat" default:"2006-01-02T15:04:05.000Z07:00"`
}

func SetUp(c LogConf) error {
	c = normalize(c)
	ws, err := syncer(c)
	if err != nil {
		return err
	}
	core := zapcore.NewCore(encoder(c), ws, level(c.Level))
	logger := zap.New(core, zap.AddCaller(), zap.AddCallerSkip(1), zap.AddStacktrace(zapcore.FatalLevel))
	zap.ReplaceGlobals(logger)
	log.SetOutput(zap.NewStdLog(logger).Writer())
	return nil
}

func ReadLastNLines(path string, n int) ([]string, error) {
	return readLastNLines(filepath.Join(path, accessFilename), n)
}

func normalize(c LogConf) LogConf {
	if c.ServiceName == "" {
		c.ServiceName = "PPanel"
	}
	if c.Mode == "" {
		c.Mode = fileMode
	}
	if c.Encoding == "" {
		c.Encoding = "json"
	}
	if c.TimeFormat == "" {
		c.TimeFormat = "2006-01-02 15:04:05.000"
	}
	if c.Path == "" {
		c.Path = "logs"
	}
	if c.Level == "" {
		c.Level = "info"
	}
	if c.KeepDays == 0 {
		c.KeepDays = c.MaxAge
	}
	if c.MaxBackups == 0 {
		c.MaxBackups = c.MaxBackup
	}
	if c.Rotation == "" {
		if c.MaxSize > 0 {
			c.Rotation = sizeRotation
		} else {
			c.Rotation = "daily"
		}
	}
	if c.FileTimeFormat == "" {
		c.FileTimeFormat = time.RFC3339
	}
	return c
}

func encoder(c LogConf) zapcore.Encoder {
	cfg := zapcore.EncoderConfig{
		TimeKey:        "timestamp",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		FunctionKey:    zapcore.OmitKey,
		MessageKey:     "content",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.LowercaseLevelEncoder,
		EncodeTime:     func(t time.Time, enc zapcore.PrimitiveArrayEncoder) { enc.AppendString(t.Format(c.TimeFormat)) },
		EncodeDuration: zapcore.StringDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
	if c.Encoding == plainEncoding {
		return zapcore.NewConsoleEncoder(cfg)
	}
	return zapcore.NewJSONEncoder(cfg)
}

func syncer(c LogConf) (zapcore.WriteSyncer, error) {
	switch c.Mode {
	case fileMode:
		return fileSyncer(c)
	case volumeMode:
		if c.ServiceName == "" {
			return nil, ErrLogServiceNameNotSet
		}
		hostname, _ := os.Hostname()
		c.Path = filepath.Join(c.Path, c.ServiceName, hostname)
		return fileSyncer(c)
	default:
		return zapcore.Lock(os.Stdout), nil
	}
}

func fileSyncer(c LogConf) (zapcore.WriteSyncer, error) {
	filename := c.FilePath
	if filename == "" {
		if c.Path == "" {
			return nil, ErrLogPathNotSet
		}
		filename = filepath.Join(c.Path, accessFilename)
	}
	w, err := newRotateWriter(filename, c)
	if err != nil {
		return nil, err
	}
	return zapcore.AddSync(w), nil
}

func level(value string) zapcore.LevelEnabler {
	switch strings.ToLower(value) {
	case "debug":
		return zapcore.DebugLevel
	case "error":
		return zapcore.ErrorLevel
	case "severe", "fatal", "alert":
		return zapcore.FatalLevel
	default:
		return zapcore.InfoLevel
	}
}

type rotateWriter struct {
	mu             sync.Mutex
	filename       string
	file           *os.File
	currentSize    int64
	currentDay     string
	maxSize        int64
	maxBackups     int
	keepDays       int
	compress       bool
	rotation       string
	fileTimeFormat string
}

func newRotateWriter(filename string, c LogConf) (*rotateWriter, error) {
	if err := os.MkdirAll(filepath.Dir(filename), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, err
	}
	return &rotateWriter{
		filename:       filename,
		file:           file,
		currentSize:    info.Size(),
		currentDay:     time.Now().Format("2006-01-02"),
		maxSize:        int64(c.MaxSize) * megabyte,
		maxBackups:     c.MaxBackups,
		keepDays:       c.KeepDays,
		compress:       c.Compress,
		rotation:       c.Rotation,
		fileTimeFormat: c.FileTimeFormat,
	}, nil
}

func (w *rotateWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.shouldRotate(len(p)) {
		if err := w.rotate(); err != nil {
			return 0, err
		}
	}
	n, err := w.file.Write(p)
	w.currentSize += int64(n)
	return n, err
}

func (w *rotateWriter) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Sync()
}

func (w *rotateWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}

func (w *rotateWriter) shouldRotate(size int) bool {
	if w.rotation == sizeRotation && w.maxSize > 0 && w.currentSize+int64(size) > w.maxSize {
		return true
	}
	day := time.Now().Format("2006-01-02")
	return w.rotation != sizeRotation && day != w.currentDay
}

func (w *rotateWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}
	backup := w.backupName()
	if _, err := os.Stat(w.filename); err == nil {
		if err = os.Rename(w.filename, backup); err != nil {
			return err
		}
		if w.compress {
			if err = gzipFile(backup); err == nil {
				_ = os.Remove(backup)
			}
		}
	}
	file, err := os.OpenFile(w.filename, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	w.file = file
	w.currentSize = 0
	w.currentDay = time.Now().Format("2006-01-02")
	w.cleanup()
	return nil
}

func (w *rotateWriter) backupName() string {
	if w.rotation == sizeRotation {
		dir, base := filepath.Split(w.filename)
		ext := filepath.Ext(base)
		prefix := strings.TrimSuffix(base, ext)
		return filepath.Join(dir, fmt.Sprintf("%s-%s%s", prefix, time.Now().Format(w.fileTimeFormat), ext))
	}
	return fmt.Sprintf("%s-%s", w.filename, time.Now().Format("2006-01-02"))
}

func (w *rotateWriter) cleanup() {
	files := w.backups()
	if w.maxBackups > 0 && len(files) > w.maxBackups {
		for _, file := range files[:len(files)-w.maxBackups] {
			_ = os.Remove(file)
		}
		files = files[len(files)-w.maxBackups:]
	}
	if w.keepDays <= 0 {
		return
	}
	boundary := time.Now().Add(-time.Duration(w.keepDays) * 24 * time.Hour)
	for _, file := range files {
		info, err := os.Stat(file)
		if err == nil && info.ModTime().Before(boundary) {
			_ = os.Remove(file)
		}
	}
}

func (w *rotateWriter) backups() []string {
	dir, base := filepath.Split(w.filename)
	ext := filepath.Ext(base)
	prefix := strings.TrimSuffix(base, ext)
	patterns := []string{
		filepath.Join(dir, prefix+"-*"+ext),
		w.filename + "-*",
	}
	seen := make(map[string]struct{})
	var files []string
	for _, pattern := range patterns {
		matches, _ := filepath.Glob(pattern)
		for _, match := range matches {
			if _, ok := seen[match]; !ok {
				seen[match] = struct{}{}
				files = append(files, match)
			}
		}
	}
	sort.Strings(files)
	return files
}

func gzipFile(filename string) error {
	src, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.OpenFile(filename+".gz", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer dst.Close()

	gz := gzip.NewWriter(dst)
	if _, err = io.Copy(gz, src); err != nil {
		_ = gz.Close()
		return err
	}
	return gz.Close()
}

func readLastNLines(filename string, n int) ([]string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return []string{}, nil
	}

	bufferSize := int64(4096)
	if bufferSize > info.Size() {
		bufferSize = info.Size()
	}
	buffer := make([]byte, bufferSize)
	position := info.Size()
	lineCount := 0

	for lineCount < n && position > 0 {
		readSize := bufferSize
		if position < bufferSize {
			readSize = position
		}
		position -= readSize
		if _, err = file.Seek(position, io.SeekStart); err != nil {
			return nil, err
		}
		if _, err = file.Read(buffer[:readSize]); err != nil {
			return nil, err
		}
		for i := readSize - 1; i >= 0; i-- {
			if buffer[i] == '\n' {
				lineCount++
				if lineCount > n {
					position += int64(i) + 1
					break
				}
			}
		}
	}
	if position < 0 {
		position = 0
	}
	if _, err = file.Seek(position, io.SeekStart); err != nil {
		return nil, err
	}

	lines := make([]string, 0, n)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
	}
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return lines, scanner.Err()
}
