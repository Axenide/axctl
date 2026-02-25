package main

import (
	"fmt"
	"os"

	"axctl/pkg/ipc/wayland/client"
	"axctl/pkg/ipc/wayland/ext_idle_notify_v1"
)

func main() {
	display, _ := client.Connect("")
	registry, _ := display.GetRegistry()

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

	cb, _ := display.Sync()
	done := make(chan struct{})
	cb.SetDoneHandler(func(e client.CallbackDoneEvent) { close(done) })
	for {
		display.Context().GetDispatch()()
		select { case <-done: goto AfterSync; default: }
	}
AfterSync:

	notif, _ := notifier.GetInputIdleNotification(0, seat)
	
	isIdle := false
	cb2, _ := display.Sync()
	done2 := make(chan struct{})
	
	// Wait, isidled might be fired BEFORE cb2.done if we set it later?
	// No, the event comes first because GetInputIdleNotification is sent before Sync.
	
	notif.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
		isIdle = true
	})
	cb2.SetDoneHandler(func(e client.CallbackDoneEvent) {
		close(done2)
	})
	
	for {
		display.Context().GetDispatch()()
		select { case <-done2: goto Finish; default: }
	}
Finish:
	fmt.Println("isIdle:", isIdle)
	os.Exit(0)
}
