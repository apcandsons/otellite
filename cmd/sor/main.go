// Command sor is the OTel Lite system of record: it ingests OTLP metrics
// and logs over HTTP, keeps them in memory for a short window, serves the
// virtual filesystem to the CLI, and optionally raises alerts.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"

	"github.com/apcandsons/otellite/internal/adapter/alertconf"
	"github.com/apcandsons/otellite/internal/adapter/fsapi"
	"github.com/apcandsons/otellite/internal/adapter/memstore"
	"github.com/apcandsons/otellite/internal/adapter/otlp"
	"github.com/apcandsons/otellite/internal/adapter/slack"
	"github.com/apcandsons/otellite/internal/domain"
	"github.com/apcandsons/otellite/internal/usecase"
)

func main() {
	listen := flag.String("listen", ":4318", "address to serve OTLP/HTTP and the CLI API on")
	retention := flag.Duration("retention", 3*time.Hour, "how long samples are kept")
	maxSamples := flag.Int("max-samples", 1_000_000, "drop the oldest samples beyond this count (0 = unlimited)")
	evictEvery := flag.Duration("evict-every", 30*time.Second, "how often the retention window is applied")
	alerts := flag.String("alerts", "", "path to alert.conf (no alerting when empty)")
	flag.Parse()

	store := memstore.New()
	ingester := usecase.NewIngester(store, domain.Budget{MaxSamples: *maxSamples})
	evictor := usecase.NewEvictor(store, domain.Window{Duration: *retention}, time.Now)
	browser := usecase.NewBrowser(store)

	var sink otlp.Sink = ingester
	if *alerts != "" {
		cfg, err := alertconf.Load(*alerts)
		if err != nil {
			log.Fatal(err)
		}
		alerter := usecase.NewAlerter(cfg.Rules, slack.New(cfg.SlackWebhooks(), nil, nil))
		sink = &tee{ingester: ingester, alerter: alerter}
		log.Printf("alerting: %d rules, %d channels from %s", len(cfg.Rules), len(cfg.Channels), *alerts)
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

// tee stores each sample and then shows it to the alerter.
type tee struct {
	ingester *usecase.Ingester
	alerter  *usecase.Alerter
}

func (t *tee) Ingest(id domain.StreamID, s domain.Sample) {
	t.ingester.Ingest(id, s)
	if err := t.alerter.Observe(id, s); err != nil {
		log.Printf("alert: %v", err)
	}
}
