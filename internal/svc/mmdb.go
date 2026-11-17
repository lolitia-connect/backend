package svc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oschwald/geoip2-golang"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/pkg/requestmeta"
	"go.uber.org/zap"
)

const GeoIPDBURL = "https://raw.githubusercontent.com/adysec/IP_database/main/geolite/GeoLite2-City.mmdb"

type IPLocation struct {
	Path string
	DB   *geoip2.Reader
}

func NewIPLocation(path string) (*IPLocation, error) {

	// 检查文件是否存在
	if _, err := os.Stat(path); os.IsNotExist(err) {
		zap.S().Infof("[GeoIP] Database not found, downloading from %s", GeoIPDBURL)
		// 文件不存在，下载数据库
		err := DownloadGeoIPDatabase(GeoIPDBURL, path)
		if err != nil {
			zap.S().Errorf("[GeoIP] Failed to download database: %v", err.Error())
			return nil, err
		}
		zap.S().Infof("[GeoIP] Database downloaded successfully")
	}

	db, err := geoip2.Open(path)
	if err != nil {
		return nil, err
	}
	return &IPLocation{
		Path: path,
		DB:   db,
	}, nil
}

func OpenIPLocation(settings config.GeoIPConfig) (*IPLocation, error) {
	if settings.Path == "" {
		settings.Path = "./cache/GeoLite2-City.mmdb"
	}
	location, err := openConfiguredIPLocation(settings)
	if err == nil {
		return location, nil
	}
	if settings.Required {
		return nil, err
	}
	zap.S().Warnf("GeoIP unavailable: %v", err)
	return &IPLocation{Path: settings.Path}, nil
}

func openConfiguredIPLocation(settings config.GeoIPConfig) (*IPLocation, error) {
	if _, err := os.Stat(settings.Path); os.IsNotExist(err) && settings.Download {
		url := settings.DownloadURL
		if url == "" {
			url = GeoIPDBURL
		}
		if err := downloadGeoIPDatabase(url, settings.Path, settings.SHA256); err != nil {
			return nil, err
		}
	}
	if err := verifyGeoIPChecksum(settings.Path, settings.SHA256); err != nil {
		return nil, err
	}
	database, err := geoip2.Open(settings.Path)
	if err != nil {
		return nil, err
	}
	return &IPLocation{Path: settings.Path, DB: database}, nil
}

func verifyGeoIPChecksum(path, expected string) error {
	if expected == "" {
		return nil
	}
	digest, err := hex.DecodeString(expected)
	if err != nil || len(digest) != sha256.Size {
		return fmt.Errorf("GeoIP SHA256 must be a 64-character hexadecimal digest")
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	if !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expected) {
		return fmt.Errorf("GeoIP checksum mismatch")
	}
	return nil
}

func (ipLoc *IPLocation) Close() error {
	if ipLoc == nil || ipLoc.DB == nil {
		return nil
	}
	return ipLoc.DB.Close()
}

// EnrichMetadata enriches request metadata with IP geolocation data.
// This is a best-effort operation: lookup failures do not affect the request.
func (ipLoc *IPLocation) EnrichMetadata(metadata requestmeta.Metadata) requestmeta.Metadata {
	if ipLoc == nil || ipLoc.DB == nil || metadata.ClientIP == "" {
		return metadata
	}

	ip := net.ParseIP(metadata.ClientIP)
	if ip == nil {
		return metadata
	}

	record, err := ipLoc.DB.City(ip)
	if err != nil {
		return metadata
	}

	if record.Country.IsoCode != "" {
		metadata.IPCountryCode = record.Country.IsoCode
	}
	if len(record.Country.Names) > 0 {
		if name, ok := record.Country.Names["en"]; ok {
			metadata.IPCountry = name
		} else if name, ok := record.Country.Names["zh-CN"]; ok {
			metadata.IPCountry = name
		}
	}
	if len(record.Subdivisions) > 0 {
		if name, ok := record.Subdivisions[0].Names["en"]; ok {
			metadata.IPRegion = name
		} else if name, ok := record.Subdivisions[0].Names["zh-CN"]; ok {
			metadata.IPRegion = name
		}
	}
	if len(record.City.Names) > 0 {
		if name, ok := record.City.Names["en"]; ok {
			metadata.IPCity = name
		} else if name, ok := record.City.Names["zh-CN"]; ok {
			metadata.IPCity = name
		}
	}

	return metadata
}

func DownloadGeoIPDatabase(url, path string) error {
	return downloadGeoIPDatabase(url, path, "")
}

func downloadGeoIPDatabase(url, path, checksum string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("GeoIP download returned HTTP %d", response.StatusCode)
	}
	const maxDatabaseBytes = 128 << 20
	if response.ContentLength > maxDatabaseBytes {
		return fmt.Errorf("GeoIP database exceeds %d bytes", maxDatabaseBytes)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	output, err := os.CreateTemp(filepath.Dir(path), ".geoip-*.mmdb")
	if err != nil {
		return err
	}
	defer os.Remove(output.Name())
	size, copyErr := io.Copy(output, io.LimitReader(response.Body, maxDatabaseBytes+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if size > maxDatabaseBytes {
		return fmt.Errorf("GeoIP database exceeds %d bytes", maxDatabaseBytes)
	}
	if err := verifyGeoIPChecksum(output.Name(), checksum); err != nil {
		return err
	}
	database, err := geoip2.Open(output.Name())
	if err != nil {
		return fmt.Errorf("invalid GeoIP database: %w", err)
	}
	if err := database.Close(); err != nil {
		return err
	}
	return os.Rename(output.Name(), path)
}
