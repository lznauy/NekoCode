// Command startupprobe measures the wall time of each startup phase of the
// standard runtime, mirroring what `nekocode --acp` does before serving.
package main

import (
	"fmt"
	"os"
	"time"

	"nekocode/bot/core"
	controlruntime "nekocode/runtime"
	"nekocode/runtime/standard"
)

func main() {
	t0 := time.Now()
	b, err := core.New()
	if err != nil {
		fmt.Println("core.New:", err)
		os.Exit(1)
	}
	t1 := time.Now()

	rt := standard.FromBot(b)
	t2 := time.Now()

	if err := rt.EnableDefaultEventRecording(); err != nil {
		fmt.Println("recording:", err)
	}
	t3 := time.Now()

	// Register the standard connectors like standard.New does.
	rt.RegisterConnector("telegram", func(r controlruntime.ConnectorRuntime) controlruntime.Connector { return nil })
	t4 := time.Now()

	fmt.Printf("core.New:            %v\n", t1.Sub(t0))
	fmt.Printf("FromBot:             %v\n", t2.Sub(t1))
	fmt.Printf("EventRecording:      %v\n", t3.Sub(t2))
	fmt.Printf("RegisterConnector:   %v\n", t4.Sub(t3))
	fmt.Printf("TOTAL:               %v\n", t4.Sub(t0))

	_ = rt.Close()
}
