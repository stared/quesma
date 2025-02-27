// Copyright Quesma, licensed under the Elastic License 2.0.
// SPDX-License-Identifier: Elastic-2.0
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"quesma/buildinfo"
	"quesma/clickhouse"
	"quesma/connectors"
	"quesma/elasticsearch"
	"quesma/feature"
	"quesma/licensing"
	"quesma/logger"
	"quesma/quesma"
	"quesma/quesma/config"
	"quesma/schema"
	"quesma/telemetry"
	"quesma/tracing"
	"syscall"
	"time"
)

const banner = `
               ________                                       
               \_____  \  __ __   ____   ______ _____ _____   
                /  / \  \|  |  \_/ __ \ /  ___//     \\__  \  
               /   \_/.  \  |  /\  ___/ \___ \|  Y Y  \/ __ \_
               \_____\ \_/____/  \___  >____  >__|_|  (____  /
                      \__>           \/     \/      \/     \/ 
`

func main() {
	println(banner)
	fmt.Printf("Quesma build info: version=[%s], build hash=[%s], build date=[%s]\n",
		buildinfo.Version, buildinfo.BuildHash, buildinfo.BuildDate)

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	doneCh := make(chan struct{})
	var cfg = config.Load()

	if err := cfg.Validate(); err != nil {
		log.Fatalf("error validating configuration: %v", err)
	}

	var asyncQueryTraceLogger *tracing.AsyncTraceLogger

	licenseMod := licensing.Init(&cfg)
	qmcLogChannel := logger.InitLogger(logger.Configuration{
		FileLogging:       cfg.Logging.FileLogging,
		Path:              cfg.Logging.Path,
		RemoteLogDrainUrl: cfg.Logging.RemoteLogDrainUrl.ToUrl(),
		Level:             cfg.Logging.Level,
		ClientId:          licenseMod.License.ClientID,
	}, sig, doneCh, asyncQueryTraceLogger)
	defer logger.StdLogFile.Close()
	defer logger.ErrLogFile.Close()

	if asyncQueryTraceLogger != nil {
		asyncQueryTraceEvictor := quesma.AsyncQueryTraceLoggerEvictor{AsyncQueryTrace: asyncQueryTraceLogger.AsyncQueryTrace}
		asyncQueryTraceEvictor.Start()
		defer asyncQueryTraceEvictor.Stop()
	}

	var connectionPool = clickhouse.InitDBConnectionPool(cfg)

	phoneHomeAgent := telemetry.NewPhoneHomeAgent(cfg, connectionPool, licenseMod.License.ClientID)
	phoneHomeAgent.Start()

	schemaManagement := clickhouse.NewSchemaManagement(connectionPool)
	schemaLoader := clickhouse.NewTableDiscovery(cfg, schemaManagement)
	schemaRegistry := schema.NewSchemaRegistry(clickhouse.TableDiscoveryTableProviderAdapter{TableDiscovery: schemaLoader}, cfg, clickhouse.SchemaTypeAdapter{})

	connManager := connectors.NewConnectorManager(cfg, connectionPool, phoneHomeAgent, schemaLoader)
	lm := connManager.GetConnector()

	im := elasticsearch.NewIndexManagement(cfg.Elasticsearch.Url.String())

	logger.Info().Msgf("loaded config: %s", cfg.String())

	instance := constructQuesma(cfg, schemaLoader, lm, im, schemaRegistry, phoneHomeAgent, qmcLogChannel)
	instance.Start()

	<-doneCh

	logger.Info().Msgf("Quesma quiting")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	feature.NotSupportedLogger.Stop()
	phoneHomeAgent.Stop(ctx)
	lm.Stop()

	instance.Close(ctx)

}

func constructQuesma(cfg config.QuesmaConfiguration, sl clickhouse.TableDiscovery, lm *clickhouse.LogManager, im elasticsearch.IndexManagement, schemaRegistry schema.Registry, phoneHomeAgent telemetry.PhoneHomeAgent, logChan <-chan logger.LogWithLevel) *quesma.Quesma {

	switch cfg.Mode {
	case config.Proxy:
		return quesma.NewQuesmaTcpProxy(phoneHomeAgent, cfg, logChan, false)
	case config.ProxyInspect:
		return quesma.NewQuesmaTcpProxy(phoneHomeAgent, cfg, logChan, true)
	case config.DualWriteQueryElastic, config.DualWriteQueryClickhouse, config.DualWriteQueryClickhouseVerify, config.DualWriteQueryClickhouseFallback:
		return quesma.NewHttpProxy(phoneHomeAgent, lm, sl, im, schemaRegistry, cfg, logChan)
	}
	logger.Panic().Msgf("unknown operation mode: %s", cfg.Mode.String())
	panic("unreachable")
}
