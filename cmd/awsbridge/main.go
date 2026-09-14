// Command awsbridge polls AWS (CloudWatch, ECS) for the resources listed in
// bridge.conf and exports each as gauges to the SoR under the stream path
// the file assigns it. It runs as a sidecar next to sor and needs only
// read-only IAM: cloudwatch:GetMetricData and ecs:DescribeServices.
//
//	awsbridge -conf /bridge.conf              # poll every `every` until SIGTERM
//	awsbridge -conf bridge.conf -validate     # parse and exit (image build check)
//
// Its own telemetry (process.uptime, memory) goes out through the ordinary
// client under OTEL_SERVICE_NAME / OTEL_RESOURCE_ATTRIBUTES, so an
// `absent` rule on the bridge's uptime covers the bridge itself.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/ecs"

	"github.com/apcandsons/otellite/client"
	"github.com/apcandsons/otellite/internal/adapter/awscw"
	"github.com/apcandsons/otellite/internal/adapter/awsecs"
	"github.com/apcandsons/otellite/internal/adapter/bridgeconf"
	"github.com/apcandsons/otellite/internal/adapter/otlpexport"
	"github.com/apcandsons/otellite/internal/usecase"
)

func main() {
	conf := flag.String("conf", "/bridge.conf", "path to bridge.conf (targets, region, poll interval)")
	validate := flag.Bool("validate", false, "parse -conf, print the target count, and exit without polling")
	flag.Parse()

	if *validate {
		summary, err := validateBridge(*conf)
		if err != nil {
			fmt.Fprintln(os.Stderr, "awsbridge:", err)
			os.Exit(1)
		}
		fmt.Println(summary)
		return
	}
	if err := run(*conf); err != nil {
		fmt.Fprintln(os.Stderr, "awsbridge:", err)
		os.Exit(1)
	}
}

func run(confPath string) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := bridgeconf.Load(confPath)
	if err != nil {
		return err
	}
	shutdown, err := client.Start(ctx, client.Options{Scope: "awsbridge"})
	if err != nil {
		return err
	}
	defer shutdown(context.Background())
	log := slog.Default()

	exporter, err := otlpexport.New(ctx, os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if err != nil {
		return err
	}
	defer exporter.Shutdown(context.Background())

	aws, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.Region))
	if err != nil {
		return fmt.Errorf("aws config: %w", err)
	}
	bridge := usecase.NewBridge(cfg.Targets,
		awscw.New(cloudwatch.NewFromConfig(aws), time.Now),
		awsecs.New(ecs.NewFromConfig(aws)),
		exporter, log)

	log.Info("awsbridge: polling", "targets", len(cfg.Targets), "region", cfg.Region, "every", cfg.Every.String(), "sor", os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	tick := time.NewTicker(cfg.Every)
	defer tick.Stop()
	for {
		started := time.Now()
		if err := bridge.Tick(ctx); err != nil {
			log.Warn("awsbridge: tick", "took", time.Since(started).Round(time.Millisecond).String(), "err", err)
		} else {
			log.Info("awsbridge: tick", "took", time.Since(started).Round(time.Millisecond).String(), "targets", len(cfg.Targets))
		}
		select {
		case <-ctx.Done():
			log.Info("awsbridge: stopping")
			return nil
		case <-tick.C:
		}
	}
}
