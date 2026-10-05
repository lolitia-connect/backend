package svc

import (
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/oschwald/geoip2-golang"
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

func (ipLoc *IPLocation) Close() error {
	return ipLoc.DB.Close()
}

// EnrichMetadata enriches request metadata with IP geolocation data.
// This is a best-effort operation: lookup failures do not affect the request.
func (ipLoc *IPLocation) EnrichMetadata(metadata requestmeta.Metadata) requestmeta.Metadata {
	if ipLoc.DB == nil || metadata.ClientIP == "" {
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

	// 创建路径, 确保目录存在
	err := os.MkdirAll(filepath.Dir(path), 0755)
	if err != nil {
		zap.S().Errorf("[GeoIP] Failed to create directory: %v", err.Error())
		return err
	}

	// 创建文件
	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()

	// 请求远程文件
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// 保存文件
	_, err = io.Copy(out, resp.Body)
	return err
}
