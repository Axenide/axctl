package main

import (
	"fmt"
	"os"

	"axctl/pkg/ipc/wayland/client"
	"axctl/pkg/ipc/wayland/ext_idle_notify_v1"
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

	var notifier *ext_idle_notify_v1.ExtIdleNotifierV1
	var seat *client.Seat

	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		if e.Interface == "wl_seat" {
			seat = client.NewSeat(display.Context())
			registry.Bind(e.Name, e.Interface, 1, seat)
		} else if e.Interface == "ext_idle_notifier_v1" {
			notifier = ext_idle_notify_v1.NewExtIdleNotifierV1(display.Context())
			registry.Bind(e.Name, e.Interface, 1, notifier)
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

	if notifier == nil {
		fmt.Println("Missing ext_idle_notifier_v1")
		os.Exit(1)
	}

	// 1 second timeout
	notification, err := notifier.GetIdleNotification(1000, seat)
	if err != nil {
		panic(err)
	}

	notification.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
		fmt.Println("IDLED!")
	})
	notification.SetResumedHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1ResumedEvent) {
		fmt.Println("RESUMED!")
	})

	fmt.Println("Waiting for idle events...")
	for {
		display.Context().GetDispatch()()
	}
}
