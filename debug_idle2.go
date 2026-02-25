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
    var notifierVersion uint32

	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		if e.Interface == "wl_seat" {
			seat = client.NewSeat(display.Context())
			registry.Bind(e.Name, e.Interface, 1, seat)
		} else if e.Interface == "ext_idle_notifier_v1" {
            notifierVersion = e.Version
            fmt.Println("Notifier global version:", e.Version)
			notifier = ext_idle_notify_v1.NewExtIdleNotifierV1(display.Context())
            ver := e.Version
            if ver > 2 { ver = 2 }
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
    if notifierVersion < 2 {
        fmt.Println("Error: input idle needs version 2, got", notifierVersion)
    }

	notif, err := notifier.GetIdleNotification(0, seat)
    fmt.Println("GetIdleNotification err:", err)
	
	isIdle := false
	cb2, _ := display.Sync()
	done2 := make(chan struct{})
	
	notif.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
        fmt.Println("IDLED!")
		isIdle = true
	})
	cb2.SetDoneHandler(func(e client.CallbackDoneEvent) {
        fmt.Println("SYNC DONE")
		close(done2)
	})
	
	for {
		err := display.Context().GetDispatch()()
        if err != nil {
            fmt.Println("Dispatch error:", err)
            break
        }
		select { case <-done2: goto Finish; default: }
	}
Finish:
	fmt.Println("isIdle:", isIdle)
	os.Exit(0)
}
