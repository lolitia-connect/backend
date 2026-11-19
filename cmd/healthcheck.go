package cmd

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/transport/httpserver"
	"github.com/perfect-panel/server/pkg/conf"
	"github.com/spf13/cobra"
)

var (
	healthcheckConfigPath string
	healthcheckTimeout    time.Duration
)

func init() {
	rootCmd.AddCommand(healthcheckCmd)
	healthcheckCmd.Flags().StringVar(&healthcheckConfigPath, "config", "config.yaml", "configuration describing the listener")
	healthcheckCmd.Flags().DurationVar(&healthcheckTimeout, "timeout", 3*time.Second, "liveness request timeout")
}

var healthcheckCmd = &cobra.Command{
	Use:          "healthcheck",
	Short:        "check the running server's liveness endpoint",
	SilenceUsage: true,
	RunE: func(command *cobra.Command, _ []string) error {
		return healthcheck(command.Context(), healthcheckConfigPath, healthcheckTimeout)
	},
}

func healthcheck(ctx context.Context, path string, timeout time.Duration) error {
	if timeout <= 0 {
		return fmt.Errorf("healthcheck timeout must be positive")
	}
	var settings config.File
	if err := conf.Load(path, &settings); err != nil {
		return fmt.Errorf("read healthcheck configuration: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, healthURL(settings), nil)
	if err != nil {
		return err
	}
	transport := &http.Transport{}
	if settings.TLS.Enable {
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: true}
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{
		Transport:     transport,
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("liveness request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("liveness endpoint answered %s", response.Status)
	}
	return nil
}

func healthURL(settings config.File) string {
	host := strings.Trim(settings.Host, "[]")
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	scheme := "http"
	if settings.TLS.Enable {
		scheme = "https"
	}
	return scheme + "://" + net.JoinHostPort(host, strconv.Itoa(settings.Port)) + httpserver.HealthzPath
}
