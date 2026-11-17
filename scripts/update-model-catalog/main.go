// Command update-model-catalog regenerates the bundled public limits snapshot.
package main

import (
	"context"
	"flag"
	"log"
	"nekocode/bot/modelcatalog"
	"os"
	"time"
)

func main() {
	output := flag.String("output", "bot/modelcatalog/snapshot.json", "snapshot destination")
	input := flag.String("input", "", "optional downloaded models.dev JSON for offline regeneration")
	flag.Parse()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var catalog *modelcatalog.Catalog
	var err error
	if *input == "" {
		catalog, err = modelcatalog.Fetch(ctx)
	} else {
		var raw []byte
		raw, err = os.ReadFile(*input)
		if err == nil {
			catalog, err = modelcatalog.Decode(raw)
		}
	}
	if err != nil {
		log.Fatal(err)
	}
	data, err := catalog.MarshalSnapshot()
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*output, append(data, '\n'), 0644); err != nil {
		log.Fatal(err)
	}
}
