// Command cli is the OTel Lite shell: it connects to a running sor and
// lets you ls, cd and cat your way through namespaces, services and
// signal streams.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/apcandsons/otellite/internal/adapter/fsapi"
	"github.com/apcandsons/otellite/internal/adapter/shell"
)

func main() {
	sor := flag.String("sor", "http://localhost:4318", "base URL of the system of record")
	flag.Parse()

	client := fsapi.NewClient(*sor, nil)
	if _, err := client.Ls("/"); err != nil {
		fmt.Fprintf(os.Stderr, "cannot reach sor at %s: %v\n", *sor, err)
		os.Exit(1)
	}
	if err := shell.New(client, nil).Run(os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
