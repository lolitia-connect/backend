package cmd

import (
	"context"
	"fmt"

	"github.com/perfect-panel/server/pkg/constant"

	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/google/uuid"
	"github.com/perfect-panel/server/pkg/hertzx"

	"github.com/perfect-panel/server/initialize"
	"github.com/perfect-panel/server/internal"
	"github.com/perfect-panel/server/internal/config"
	"github.com/perfect-panel/server/internal/plugin"
	"github.com/perfect-panel/server/internal/svc"
	"github.com/perfect-panel/server/pkg/conf"
	"github.com/perfect-panel/server/pkg/logging"
	"github.com/perfect-panel/server/pkg/orm"
	"github.com/perfect-panel/server/pkg/service"
	"github.com/perfect-panel/server/pkg/tool"
	"github.com/perfect-panel/server/queue"
	"github.com/perfect-panel/server/scheduler"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
)

func init() {
	startCmd.Flags().StringVar(&startConfigPath, "config", "config.yaml", "config.yaml file to read from")
}

var (
	startConfigPath string
)

var startCmd = &cobra.Command{
	Use:   "run",
	Short: "start PPanel",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("[PPanel version] v" + fmt.Sprintf("%s(%s)", constant.Version, constant.BuildTime))
		return run()
	},
}

func run() error {
	services, resources := getServers()
	defer func() {
		if err := resources.Close(); err != nil {
			zap.S().Errorf("close service resources: %v", err)
		}
	}()
	defer services.Stop()
	result := make(chan error, 1)
	go func() { result <- services.Run() }()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(quit)
	select {
	case err := <-result:
		return err
	case <-quit:
		services.Stop()
		return <-result
	}
}
func getServers() (*service.Group, *svc.ServiceContext) {
	var c config.Config

	// check config file is exist
	if _, err := os.Stat(startConfigPath); os.IsNotExist(err) {
		if dir := filepath.Dir(startConfigPath); dir != "." {
			if err = os.MkdirAll(dir, 0750); err != nil {
				log.Fatalf("Please create the directory %s and place the configuration file %s in it.\n", dir, startConfigPath)
			}
		}
		// create new config file
		file, err := os.OpenFile(startConfigPath, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			zap.S().Errorf("Please create the configuration file %s.\n", startConfigPath)
			panic(fmt.Sprintf("Please create the configuration file %s.\n", startConfigPath))
		}
		_ = file.Close()
	}
	// check config file is empty, if empty, start init web server
	if initConfig(&c) {
		status, server, err := initialize.Config(startConfigPath)
		if err != nil {
			panic(err)
		}
		if completed := <-status; !completed {
			panic("setup listener stopped before installation completed")
		}
		if err := server.Shutdown(context.TODO()); err != nil {
			log.Printf("Init Server Shutdown: %s\n", err.Error())
		}
	}
	conf.MustLoad(startConfigPath, &c)
	if c.JwtAuth.AccessSecret == "" {
		panic("JwtAuth.AccessSecret must not be empty")
	}
	if !c.Debug {
		hertzx.SetMode(hertzx.ReleaseMode)
	}
	// init logger
	if err := logging.SetUp(c.Logger); err != nil {
		zap.S().Errorf("Logger setup failed: %v", err.Error())
	}

	// init service context
	ctx := svc.NewServiceContext(c)
	prepared := false
	defer func() {
		if !prepared {
			_ = ctx.Close()
		}
	}()

	// init plugin manager
	pluginHostEnv := &plugin.HostEnv{
		Config: c,
		Redis:  plugin.NewRedisAdapter(ctx.Redis),
		Store:  plugin.NewStoreAdapter(ctx.SQLDB, ctx.SQLDialect),
		Queue:  plugin.NewQueueAdapter(ctx.Queue),
	}
	pluginMgr := plugin.NewManager(pluginHostEnv)
	ctx.PluginReady = pluginMgr
	ctx.PluginMgr = pluginMgr

	services := service.NewServiceGroup()
	services.Add(pluginMgr)
	services.Add(queue.NewService(ctx))
	services.Add(scheduler.NewService(ctx))
	services.Add(internal.NewService(ctx))
	prepared = true
	return services, ctx
}

func initConfig(c *config.Config) bool {
	conf.MustLoad(startConfigPath, c)
	if c.DatabaseConfig().Addr != "" {
		return false
	}
	dsn, uri := os.Getenv("PPANEL_DB"), os.Getenv("PPANEL_REDIS")
	if dsn == "" || uri == "" {
		return true
	}
	database := orm.ParseDSN(dsn)
	if database == nil {
		return true
	}
	addr, password, databaseIndex, err := tool.ParseRedisURI(uri)
	if err != nil {
		return true
	}
	if c.JwtAuth.AccessSecret == "" {
		c.JwtAuth.AccessSecret = uuid.New().String()
	}
	c.SetDatabaseConfig(*database)
	c.Redis.Host, c.Redis.Pass, c.Redis.DB = addr, password, databaseIndex
	if err := persistEnvironmentConfig(startConfigPath, c); err != nil {
		panic(fmt.Errorf("persist installation configuration: %w", err))
	}
	return false
}

func persistEnvironmentConfig(path string, settings *config.Config) error {
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var document yaml.Node
	if err := yaml.Unmarshal(content, &document); err != nil {
		return err
	}
	if len(document.Content) == 0 {
		document = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode, Tag: "!!map"}}}
	}
	mapping := document.Content[0]
	if mapping.Kind != yaml.MappingNode {
		return fmt.Errorf("configuration must be a YAML mapping")
	}
	for _, replacement := range []struct {
		key   string
		value any
	}{{"Database", settings.DatabaseConfig()}, {"Redis", settings.Redis}, {"JwtAuth", settings.JwtAuth}} {
		value := &yaml.Node{}
		if err := value.Encode(replacement.value); err != nil {
			return err
		}
		found := false
		for index := 0; index < len(mapping.Content); index += 2 {
			if mapping.Content[index].Value == replacement.key {
				mapping.Content[index+1] = value
				found = true
				break
			}
		}
		if !found {
			mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: replacement.key}, value)
		}
	}
	for index := 0; index < len(mapping.Content); index += 2 {
		if mapping.Content[index].Value == "MySQL" {
			mapping.Content = append(mapping.Content[:index], mapping.Content[index+2:]...)
			break
		}
	}
	encoded, err := yaml.Marshal(&document)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return err
	}
	return os.WriteFile(path, encoded, 0600)
}
