// Package mock provides test utilities and mock implementations for the ipc package.
//
// The Compositor mock allows you to:
//   - Simulate compositor behavior without real sockets
//   - Inject test windows and workspaces
//   - Control success/error responses
//   - Track method calls for verification
//   - Emit synthetic events
//
// Example usage:
//
//	m := mock.NewCompositor()
//	m.AddWindow(ipc.Window{ID: "win1", Title: "Test"})
//	m.AddWorkspace(ipc.Workspace{ID: "ws1", Name: "Work"})
//
//	// Simulate successful operations
//	err := m.FocusWindow("win1")
//	if err != nil {
//		log.Fatal(err)
//	}
//
//	// Verify calls were made
//	calls := m.FocusWindowCalls()
//	assert.Equal(t, []string{"win1"}, calls)
//
//	// Simulate errors
//	m.SetListWindowsError(ipc.ErrCompositorNotAvailable)
//	_, err := m.ListWindows()
//	assert.Equal(t, ipc.ErrCompositorNotAvailable, err)
//
//	// Subscribe and emit events
//	ch, _ := m.Subscribe()
//	m.EmitEvent(ipc.Event{
//		Type: ipc.EventWindowCreated,
//		Window: &ipc.Window{ID: "win2", Title: "New"},
//	})
//
//	// Receive event
//	evt := <-ch
//	assert.Equal(t, "win2", evt.Window.ID)
//
//	m.Close()
package mock
