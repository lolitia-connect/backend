package httpserver

import (
	"github.com/cloudwego/hertz/pkg/common/config"
	"github.com/cloudwego/hertz/pkg/network"
	"github.com/cloudwego/hertz/pkg/network/standard"
)

func platformTransport(options *config.Options) network.Transporter {
	return standard.NewTransporter(options)
}
