package main

import (
	"fmt"
	"os"

	"axctl/pkg/ipc/wayland/client"
	"axctl/pkg/ipc/wayland/idle_inhibit_v1"
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

	var compositor *client.Compositor
	var inhibitorMgr *idle_inhibit_v1.ZwpIdleInhibitManagerV1

	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		if e.Interface == "wl_compositor" {
			compositor = client.NewCompositor(display.Context())
			registry.Bind(e.Name, e.Interface, 1, compositor)
		} else if e.Interface == "zwp_idle_inhibit_manager_v1" {
			inhibitorMgr = idle_inhibit_v1.NewZwpIdleInhibitManagerV1(display.Context())
			registry.Bind(e.Name, e.Interface, 1, inhibitorMgr)
		}
	})

	callback, _ := display.Sync()
	done := make(chan struct{})
	callback.SetDoneHandler(func(e client.CallbackDoneEvent) { close(done) })

	for {
		display.Context().GetDispatch()()
		select {
		case <-done:
			goto AfterSync
		default:
		}
	}
AfterSync:

	if compositor == nil || inhibitorMgr == nil {
		fmt.Println("Missing required globals")
		os.Exit(1)
	}

	surface, _ := compositor.CreateSurface()

	inhibitor, err := inhibitorMgr.CreateInhibitor(surface)
	if err != nil {
		panic(err)
	}

	fmt.Println("Inhibitor created! ID:", inhibitor.ID())
	select {}
}
