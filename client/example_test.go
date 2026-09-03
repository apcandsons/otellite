package client_test

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/apcandsons/otellite/client"
)

// The composition root calls Start once; everything else takes its
// instruments from the package-level helpers. With no
// OTEL_EXPORTER_OTLP_ENDPOINT the same program runs with telemetry off.
func ExampleStart() {
	ctx := context.Background()
	shutdown, err := client.Start(ctx, client.Options{Scope: "iam/iam-api"})
	if err != nil {
		log.Fatal(err)
	}
	defer shutdown(ctx)

	denies, _ := client.NewCounterSet("authz.deny", "no_grant", "explicit_deny")
	denies.Add(ctx, "no_grant", 1)
	client.Gauge("sync.lag", "1", "sequence gap to the source", func() float64 { return 0 })

	mux := http.NewServeMux()
	_ = client.HTTPMiddleware(mux)
	client.Logger().Info("ready", "port", 8080)
	fmt.Println("started")
	// Output: started
}
