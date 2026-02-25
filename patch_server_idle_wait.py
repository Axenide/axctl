import sys

with open('pkg/server/idle.go', 'r') as f:
    content = f.read()

target = '''func (im *IdleManager) waitResumeInternal(timeoutMs uint32, inputOnly bool) error {
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
		notif.SetResumedHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1ResumedEvent) {
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
}'''

new_code = '''func (im *IdleManager) waitResumeInternal(timeoutMs uint32, inputOnly bool) error {
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
			// DO NOTHING. We just need to handle it so it doesn't cause issues if we receive it.
			// Actually wait, if the notification is NOT currently idle, the compositor will NOT send a resumed event
			// when we move the mouse. It will ONLY send a resumed event AFTER it has sent an idled event!
		})
		
		notif.SetResumedHandler(func(e ext_idle_notify_v1.ExtIdleNotificationV1ResumedEvent) {
			select {
			case ch <- struct{}{}:
			default:
			}
		})
	}
	
	// Wait, we need to guarantee that the compositor considers this object "idled" before it can send "resumed".
	// The problem in your test script is: you do input-idle-wait (which creates an object, waits for idled, then DESTROYS IT).
	// Then you do input-resume-wait (which creates a NEW object, and waits for resumed).
	// But because it's a new object, it starts as "not idle"! So when you move the mouse, the compositor says "it's not idle anyway, so no resumed event".
	// To fix this, waitResumeInternal MUST wait for the idled event FIRST if it hasn't happened!
	im.wlMu.Unlock()

	if err != nil {
		return err
	}

	defer func() {
		im.wlMu.Lock()
		notif.Destroy()
		im.wlMu.Unlock()
	}()

	// Wait, waitResume blocks until resumed. If the object was just created, it will first become idle,
	// THEN become resumed. But the script expects it to be ALREADY idle.
	// Since Wayland events are asynchronous, the object will immediately fire 'idled' if the time has already passed.
	// Then when we move the mouse, it fires 'resumed'.
	// So we just need to wait for resumed!
	
	<-ch
	return nil
}'''

if target in content:
    content = content.replace(target, new_code)
else:
    print("Target not found")
    sys.exit(1)

with open('pkg/server/idle.go', 'w') as f:
    f.write(content)
