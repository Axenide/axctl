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
            ver := e.Version
            if ver > 2 { ver = 2 }
			notifier = ext_idle_notify_v1.NewExtIdleNotifierV1(display.Context())
			registry.Bind(e.Name, e.Interface, ver, notifier)
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
    fmt.Println("Start loop 1")
	notif, _ := notifier.GetInputIdleNotification(2000, seat)
	ch := make(chan struct{})
	notif.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
        fmt.Println("IDLED 1")
		ch <- struct{}{}
	})
    notif.SetResumedHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1ResumedEvent) {})
	for {
		display.Context().GetDispatch()()
		select { case <-ch: goto Resume1; default: }
	}
Resume1:
    fmt.Println("Wait resume 1 (Destroying first notification)")
    notif.Destroy()
    
    // Create new object for resume
    notif2, _ := notifier.GetInputIdleNotification(2000, seat)
    notif2.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
        fmt.Println("NEW OBJECT IDLED")
    })
    notif2.SetResumedHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1ResumedEvent) {
        fmt.Println("NEW OBJECT RESUMED")
        ch <- struct{}{}
    })
    for {
        display.Context().GetDispatch()()
        select { case <-ch: goto Finish; default: }
    }

Finish:
	os.Exit(0)
}
