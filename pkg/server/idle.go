package server

import (
	"fmt"
	"sync"
	"time"

	"axctl/pkg/ipc/wayland/client"
	"axctl/pkg/ipc/wayland/ext_idle_notify_v1"
	"axctl/pkg/ipc/wayland/idle_inhibit_v1"
)

type IdleManager struct {
	display      *client.Display
	compositor   *client.Compositor
	seat         *client.Seat
	notifier     *ext_idle_notify_v1.ExtIdleNotifierV1
	inhibitorMgr *idle_inhibit_v1.ZwpIdleInhibitManagerV1

	mu          sync.Mutex
	wlMu        sync.Mutex // Protects Wayland socket writes
	inhibitor   *idle_inhibit_v1.ZwpIdleInhibitorV1
	idleSurface *client.Surface
}

func NewIdleManager() (*IdleManager, error) {
	display, err := client.Connect("")
	if err != nil {
		return nil, fmt.Errorf("wayland connect failed: %v", err)
	}
	registry, err := display.GetRegistry()
	if err != nil {
		return nil, fmt.Errorf("get registry failed: %v", err)
	}

	im := &IdleManager{
		display: display,
	}

	registry.SetGlobalHandler(func(e client.RegistryGlobalEvent) {
		switch e.Interface {
		case "wl_compositor":
			im.compositor = client.NewCompositor(display.Context())
			registry.Bind(e.Name, e.Interface, 1, im.compositor)
		case "wl_seat":
			im.seat = client.NewSeat(display.Context())
			registry.Bind(e.Name, e.Interface, 1, im.seat)
		case "ext_idle_notifier_v1":
			im.notifier = ext_idle_notify_v1.NewExtIdleNotifierV1(display.Context())
			ver := e.Version
			if ver > 2 {
				ver = 2
			}
			registry.Bind(e.Name, e.Interface, ver, im.notifier)
		case "zwp_idle_inhibit_manager_v1":
			im.inhibitorMgr = idle_inhibit_v1.NewZwpIdleInhibitManagerV1(display.Context())
			registry.Bind(e.Name, e.Interface, 1, im.inhibitorMgr)
		}
	})

	callback, err := display.Sync()
	if err != nil {
		return nil, err
	}
	done := make(chan struct{})
	callback.SetDoneHandler(func(e client.CallbackDoneEvent) { close(done) })

	for {
		if err := display.Context().GetDispatch()(); err != nil {
			return nil, err
		}
		select {
		case <-done:
			goto AfterSync
		default:
		}
	}
AfterSync:
	go func() {
		for {
			dispatchFunc := display.Context().GetDispatch()
			// We MUST NOT hold im.wlMu while executing dispatchFunc,
			// because handlers (like idledHandler) might try to do something,
			// though currently they just set a boolean or close a channel.
			if err := dispatchFunc(); err != nil {
				return
			}
		}
	}()

	return im, nil
}

func (im *IdleManager) Inhibit(on bool) error {
	im.mu.Lock()
	defer im.mu.Unlock()

	im.wlMu.Lock()
	defer im.wlMu.Unlock()

	if on {
		if im.inhibitor != nil {
			return nil // already inhibited
		}
		if im.compositor == nil || im.inhibitorMgr == nil {
			return fmt.Errorf("idle_inhibit not supported by compositor")
		}
		im.idleSurface, _ = im.compositor.CreateSurface()
		inhibitor, err := im.inhibitorMgr.CreateInhibitor(im.idleSurface)
		if err != nil {
			return err
		}
		im.inhibitor = inhibitor
	} else {
		if im.inhibitor != nil {
			im.inhibitor.Destroy()
			im.inhibitor = nil
		}
		if im.idleSurface != nil {
			im.idleSurface.Destroy()
			im.idleSurface = nil
		}
	}
	return nil
}

func (im *IdleManager) waitIdleInternal(timeoutMs uint32, inputOnly bool) error {
	if im.notifier == nil || im.seat == nil {
		return fmt.Errorf("idle_notify not supported by compositor")
	}

	ch := make(chan struct{})

	im.wlMu.Lock()
	var notif *ext_idle_notify_v1.ExtIdleNotificationV1
	var err error
	if inputOnly {
		notif, err = im.notifier.GetInputIdleNotification(timeoutMs, im.seat)
	} else {
		notif, err = im.notifier.GetIdleNotification(timeoutMs, im.seat)
	}

	if err == nil {
		notif.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
			select {
			case ch <- struct{}{}:
			default:
			}
		})
	}
	im.wlMu.Unlock()

	if err != nil {
		return err
	}

	defer func() {
		im.wlMu.Lock()
		notif.Destroy()
		im.wlMu.Unlock()
	}()

	<-ch
	return nil
}

func (im *IdleManager) WaitIdle(timeoutMs uint32) error {
	return im.waitIdleInternal(timeoutMs, false)
}

func (im *IdleManager) waitResumeInternal(timeoutMs uint32, inputOnly bool) error {
	if im.notifier == nil || im.seat == nil {
		return fmt.Errorf("idle_notify not supported by compositor")
	}

	ch := make(chan struct{})

	im.wlMu.Lock()
	var notif *ext_idle_notify_v1.ExtIdleNotificationV1
	var err error

	// A timeout of 0 means we get notified the exact moment the seat is inactive.
	// Since humans can't input continuously at the microsecond level, a 0ms listener
	// will almost immediately fire 'idled' if the user isn't actively moving the mouse.
	// Then, the moment they move it, it fires 'resumed'.
	// This makes 'resume-wait' perfectly reliable regardless of the original timeout!
	if inputOnly {
		notif, err = im.notifier.GetInputIdleNotification(0, im.seat)
	} else {
		notif, err = im.notifier.GetIdleNotification(0, im.seat)
	}

	if err != nil {
		im.wlMu.Unlock()
		return err
	}

	var hasIdled bool
	notif.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
		im.mu.Lock()
		hasIdled = true
		im.mu.Unlock()
	})

	notif.SetResumedHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1ResumedEvent) {
		im.mu.Lock()
		ready := hasIdled
		im.mu.Unlock()
		if ready {
			select {
			case ch <- struct{}{}:
			default:
			}
		}
	})

	callback, _ := im.display.Sync()
	done := make(chan struct{})
	callback.SetDoneHandler(func(e client.CallbackDoneEvent) { close(done) })
	im.wlMu.Unlock()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}

	// We wait briefly. If 'idled' hasn't fired for a 0ms timer, the user is ACTIVELY
	// spamming the keyboard/mouse right now. Thus, the system is already resumed.
	time.Sleep(50 * time.Millisecond)

	im.mu.Lock()
	currentlyIdle := hasIdled
	im.mu.Unlock()

	if !currentlyIdle {
		im.wlMu.Lock()
		notif.Destroy()
		im.wlMu.Unlock()
		return nil
	}

	defer func() {
		im.wlMu.Lock()
		notif.Destroy()
		im.wlMu.Unlock()
	}()

	<-ch
	return nil
}

func (im *IdleManager) isIdleInternal(timeoutMs uint32, inputOnly bool) (bool, error) {
	if im.notifier == nil || im.seat == nil {
		return false, fmt.Errorf("idle_notify not supported by compositor")
	}

	isIdle := false
	done := make(chan struct{})

	im.wlMu.Lock()
	var notif *ext_idle_notify_v1.ExtIdleNotificationV1
	var err error
	if inputOnly {
		notif, err = im.notifier.GetInputIdleNotification(timeoutMs, im.seat)
	} else {
		notif, err = im.notifier.GetIdleNotification(timeoutMs, im.seat)
	}
	if err != nil {
		im.wlMu.Unlock()
		return false, err
	}

	// ALWAYS set the handler BEFORE unlocking wlMu, because the background
	// dispatcher might receive the event immediately!
	notif.SetIdledHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1IdledEvent) {
		isIdle = true
	})
	notif.SetResumedHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1ResumedEvent) {
	})

	// To avoid blocking forever if something goes wrong with Wayland sync
	// we will use a timeout fallback. But normally Sync is instant.
	callback, err := im.display.Sync()
	if err != nil {
		notif.Destroy()
		im.wlMu.Unlock()
		return false, err
	}
	callback.SetDoneHandler(func(e client.CallbackDoneEvent) {
		close(done)
	})
	im.wlMu.Unlock()

	// Wait for the sync callback to finish, or timeout if it deadlocks
	// The background dispatch loop handles events. It will close 'done' when Sync finishes.
	// If the socket was closed, the background loop might exit WITHOUT dispatching our callback!
	select {
	case <-done:
		// Normal completion
	case <-time.After(2 * time.Second):
		// Timeout - the compositor is unresponsive or connection died
		return false, fmt.Errorf("timeout waiting for idle sync callback")
	}

	im.wlMu.Lock()
	notif.Destroy()
	im.wlMu.Unlock()

	return isIdle, nil
}

func (im *IdleManager) IsIdle(timeoutMs uint32) (bool, error) {
	return im.isIdleInternal(timeoutMs, false)
}

func (im *IdleManager) IsInhibited() bool {
	im.mu.Lock()
	defer im.mu.Unlock()
	return im.inhibitor != nil
}

func (im *IdleManager) WaitInputIdle(timeoutMs uint32) error {
	return im.waitIdleInternal(timeoutMs, true)
}

func (im *IdleManager) WaitInputResume(timeoutMs uint32) error {
	return im.waitResumeInternal(timeoutMs, true)
}

func (im *IdleManager) IsInputIdle(timeoutMs uint32) (bool, error) {
	return im.isIdleInternal(timeoutMs, true)
}

func (im *IdleManager) WaitResume(timeoutMs uint32) error {
	return im.waitResumeInternal(timeoutMs, false)
}
