import re

with open('/home/adriano/Repos/Axenide/axctl/pkg/ipc/mangowc/client.go', 'r') as f:
    content = f.read()

old_func = '''func (m *Mangowc) roundtrip() error {
	if m.protoErr != nil {
		return m.protoErr
	}
	callback, err := m.display.Sync()
	if err != nil {
		return err
	}
	done := make(chan struct{})
	callback.SetDoneHandler(func(client.CallbackDoneEvent) {
		close(done)
	})
	for {
		if err := m.display.Context().GetDispatch()(); err != nil {
			if m.protoErr != nil {
				return m.protoErr
			}
			return err
		}
		if m.protoErr != nil {
			return m.protoErr
		}
		select {
		case <-done:
			return nil
		default:
		}
	}
}'''

new_func = '''func (m *Mangowc) roundtrip() error {
	if err := m.display.Roundtrip(); err != nil {
		if m.protoErr != nil {
			return m.protoErr
		}
		return err
	}
	if m.protoErr != nil {
		return m.protoErr
	}
	return nil
}'''

# Also fix the Subscribe goroutine
old_sub = '''	go func() {
		for {
			if err := m.display.Context().GetDispatch()(); err != nil {
				m.mu.Lock()
				close(m.eventCh)
				m.subscribed = false
				m.mu.Unlock()
				return
			}
		}
	}()'''

new_sub = '''	go func() {
		for {
			dispatchFunc := m.display.Context().GetDispatch()
			if err := dispatchFunc(); err != nil {
				m.mu.Lock()
				close(m.eventCh)
				m.subscribed = false
				m.mu.Unlock()
				return
			}
		}
	}()'''

content = content.replace(old_func, new_func)
content = content.replace(old_sub, new_sub)

with open('/home/adriano/Repos/Axenide/axctl/pkg/ipc/mangowc/client.go', 'w') as f:
    f.write(content)
