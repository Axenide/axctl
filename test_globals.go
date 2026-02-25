package main

import (
	"fmt"
	"os"

	"axctl/pkg/ipc/wayland/client"
)

func main() {
	display, err := client.Connect("")
	if err != nil {
		panic(err)
	}

	registry, err := display.GetRegistry()
	if err != nil {
		panic(err)
	}

	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		fmt.Printf("Global: %s v%d\\n", e.Interface, e.Version)
	})

	callback, _ := display.Sync()
	done := make(chan struct{})
	callback.SetDoneHandler(func(e client.CallbackDoneEvent) {
		close(done)
	})

	for {
		if err := display.Context().GetDispatch()(); err != nil {
			panic(err)
		}
		select {
		case <-done:
			os.Exit(0)
		default:
		}
	}
}
