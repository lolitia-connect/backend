//go:build !windows

package httpserver

import (
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/network/netpoll"
	"github.com/cloudwego/hertz/pkg/network/standard"
)

func platformTransport(options *config.Options) network.Transporter {
	if options.TLS != nil {
		return standard.NewTransporter(options)
	}
	return netpoll.NewTransporter(options)
}
