// Command sor is the OTel Lite system of record: it ingests OTLP metrics
// and logs over HTTP, keeps them in memory for a short window, serves the
// virtual filesystem to the CLI over HTTP and to the web UI over gRPC, and
// optionally raises alerts.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"time"

	"google.golang.org/grpc"

	"github.com/apcandsons/otellite/internal/adapter/alertconf"
	"github.com/apcandsons/otellite/internal/adapter/fsapi"
	"github.com/apcandsons/otellite/internal/adapter/grpcapi"
	"github.com/apcandsons/otellite/internal/adapter/memstore"
	"github.com/apcandsons/otellite/internal/adapter/otlp"
	"github.com/apcandsons/otellite/internal/adapter/slack"
	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

func main() {
	listen := flag.String("listen", ":4318", "address to serve OTLP/HTTP and the CLI API on")
	grpcListen := flag.String("grpc", ":4319", "address to serve the gRPC API (web UI) on; empty disables it")
	retention := flag.Duration("retention", 3*time.Hour, "how long samples are kept")
	maxSamples := flag.Int("max-samples", 1_000_000, "drop the oldest samples beyond this count (0 = unlimited)")
	evictEvery := flag.Duration("evict-every", 30*time.Second, "how often the retention window is applied")
	alerts := flag.String("alerts", "", "path to alert.conf (no alerting when empty)")
	checkEvery := flag.Duration("check-every", time.Second, "how often absent rules are evaluated")
	validate := flag.Bool("validate", false, "parse -alerts, print the rule and channel counts, and exit without serving")
	flag.Parse()

	if *validate {
		summary, err := validateAlerts(*alerts)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(summary)
		return
	}

	store := memstore.New()
	ingester := usecase.NewIngester(store, domain.Budget{MaxSamples: *maxSamples})
	evictor := usecase.NewEvictor(store, domain.Window{Duration: *retention}, time.Now)
	browser := usecase.NewBrowser(store)

	feed := usecase.NewFeed(256)
	sink := &tee{ingester: ingester, feed: feed}
	var alertStatus grpcapi.Alerts
	if *alerts != "" {
		cfg, err := alertconf.Load(*alerts)
		if err != nil {
			log.Fatal(err)
		}
		notifier := usecase.Notifiers{slack.New(destinations(cfg), nil, nil), feed}
		alerter := usecase.NewAlerter(cfg.Rules, notifier)
		sink.alerter = alerter
		alertStatus = alerter
		go func() {
			for range time.Tick(*checkEvery) {
				if err := alerter.Check(time.Now()); err != nil {
					log.Printf("alert: %v", err)
				}
			}
		}()
		log.Printf("alerting: %d rules, %d channels from %s", len(cfg.Rules), len(cfg.Channels), *alerts)
	}

	if *grpcListen != "" {
		lis, err := net.Listen("tcp", *grpcListen)
		if err != nil {
			log.Fatal(err)
		}
		gs := grpc.NewServer()
		grpcapi.New(browser, alertStatus, feed).Register(gs)
		go func() { log.Fatal(gs.Serve(lis)) }()
		log.Printf("grpc listening on %s", *grpcListen)
	}

	go func() {
		for range time.Tick(*evictEvery) {
			if n := evictor.Run(); n > 0 {
				log.Printf("forgot %d samples older than %s", n, *retention)
			}
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/v1/", otlp.NewHandler(sink, time.Now))
	mux.Handle("/fs/", fsapi.NewHandler(browser))

	log.Printf("sor listening on %s (retention %s, max %d samples)", *listen, *retention, *maxSamples)
	log.Fatal(http.ListenAndServe(*listen, mux))
}

// destinations maps channel names from alert.conf to Slack destinations.
func destinations(cfg alertconf.Config) map[string]slack.Destination {
	out := map[string]slack.Destination{}
	for _, ch := range cfg.Channels {
		out[ch.Name] = slack.Destination{Webhook: ch.URL, Token: ch.Token, ChannelID: ch.ChannelID}
	}
	return out
}

// tee stores each sample, publishes it to the live feed, and then shows
// it to the alerter (when alerting is configured).
type tee struct {
	ingester *usecase.Ingester
	feed     *usecase.Feed
	alerter  *usecase.Alerter
}

func (t *tee) Ingest(id domain.StreamID, s domain.Sample) {
	t.ingester.Ingest(id, s)
	t.feed.Ingest(id, s)
	if t.alerter == nil {
		return
	}
	if err := t.alerter.Observe(id, s); err != nil {
		log.Printf("alert: %v", err)
	}
}
