// Hand-written Go bindings for wlr-foreign-toplevel-management-unstable-v1
// Based on: pkg/ipc/wayland/protocols/wlr-foreign-toplevel-management-unstable-v1.xml
// Following the exact patterns from pkg/ipc/mangowc/dwlipc/dwl_ipc.go
//
// wlr_foreign_toplevel_management_unstable_v1 Protocol Copyright:
//
// Copyright © 2018 Ilia Bozhinov
//
// Permission to use, copy, modify, distribute, and sell this
// software and its documentation for any purpose is hereby granted
// without fee, provided that the above copyright notice appear in
// all copies and that both that copyright notice and this permission
// notice appear in supporting documentation, and that the name of
// the copyright holders not be used in advertising or publicity
// pertaining to distribution of the software without specific,
// written prior permission.  The copyright holders make no
// representations about the suitability of this software for any
// purpose.  It is provided "as is" without express or implied
// warranty.

package foreign_toplevel_v1

import "axctl/pkg/ipc/wayland/client"

// ToplevelState represents the state flags for a foreign toplevel handle.
type ToplevelState uint32

const (
	ToplevelStateMaximized  ToplevelState = 0
	ToplevelStateMinimized  ToplevelState = 1
	ToplevelStateActivated  ToplevelState = 2
	ToplevelStateFullscreen ToplevelState = 3
)

// ---------------------------------------------------------------------------
// ForeignToplevelManagerV1 : list and control opened apps
//
// After a client binds the zwlr_foreign_toplevel_manager_v1, each opened
// toplevel window will be sent via the toplevel event.
// ---------------------------------------------------------------------------

type ForeignToplevelManagerV1 struct {
	client.BaseProxy
	toplevelHandler ForeignToplevelManagerV1ToplevelHandlerFunc
	finishedHandler ForeignToplevelManagerV1FinishedHandlerFunc
}

func NewForeignToplevelManagerV1(ctx *client.Context) *ForeignToplevelManagerV1 {
	m := &ForeignToplevelManagerV1{}
	ctx.Register(m)
	return m
}

// Stop : stop sending events (request opcode 0)
//
// Indicates the client no longer wishes to receive events for new toplevels.
func (i *ForeignToplevelManagerV1) Stop() error {
	const opcode = 0
	const _reqBufLen = 8
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// ForeignToplevelManagerV1ToplevelEvent : a toplevel has been created
type ForeignToplevelManagerV1ToplevelEvent struct {
	Toplevel *ForeignToplevelHandleV1
}
type ForeignToplevelManagerV1ToplevelHandlerFunc func(ForeignToplevelManagerV1ToplevelEvent)

func (i *ForeignToplevelManagerV1) SetToplevelHandler(f ForeignToplevelManagerV1ToplevelHandlerFunc) {
	i.toplevelHandler = f
}

// ForeignToplevelManagerV1FinishedEvent : the compositor has finished
type ForeignToplevelManagerV1FinishedEvent struct{}
type ForeignToplevelManagerV1FinishedHandlerFunc func(ForeignToplevelManagerV1FinishedEvent)

func (i *ForeignToplevelManagerV1) SetFinishedHandler(f ForeignToplevelManagerV1FinishedHandlerFunc) {
	i.finishedHandler = f
}

func (i *ForeignToplevelManagerV1) Dispatch(opcode uint32, fd int, data []byte) {
	switch opcode {
	case 0: // toplevel (new_id)
		// Server created a new toplevel handle — register it with the
		// server-assigned ID so subsequent events for this object are routed.
		l := 0
		id := client.Uint32(data[l : l+4])
		handle := &ForeignToplevelHandleV1{}
		i.Context().RegisterWithID(handle, id)
		if i.toplevelHandler != nil {
			i.toplevelHandler(ForeignToplevelManagerV1ToplevelEvent{Toplevel: handle})
		}
	case 1: // finished
		if i.finishedHandler != nil {
			i.finishedHandler(ForeignToplevelManagerV1FinishedEvent{})
		}
	}
}

// ---------------------------------------------------------------------------
// ForeignToplevelHandleV1 : an opened toplevel
//
// Represents a single opened toplevel window. Each app may have multiple
// opened toplevels. Events are batched and committed with a "done" event.
// ---------------------------------------------------------------------------

type ForeignToplevelHandleV1 struct {
	client.BaseProxy
	titleHandler       ForeignToplevelHandleV1TitleHandlerFunc
	appIdHandler       ForeignToplevelHandleV1AppIdHandlerFunc
	outputEnterHandler ForeignToplevelHandleV1OutputEnterHandlerFunc
	outputLeaveHandler ForeignToplevelHandleV1OutputLeaveHandlerFunc
	stateHandler       ForeignToplevelHandleV1StateHandlerFunc
	doneHandler        ForeignToplevelHandleV1DoneHandlerFunc
	closedHandler      ForeignToplevelHandleV1ClosedHandlerFunc
	parentHandler      ForeignToplevelHandleV1ParentHandlerFunc
}

// --- Requests ---

// SetMaximized : request opcode 0
func (i *ForeignToplevelHandleV1) SetMaximized() error {
	const opcode = 0
	const _reqBufLen = 8
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// UnsetMaximized : request opcode 1
func (i *ForeignToplevelHandleV1) UnsetMaximized() error {
	const opcode = 1
	const _reqBufLen = 8
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// SetMinimized : request opcode 2
func (i *ForeignToplevelHandleV1) SetMinimized() error {
	const opcode = 2
	const _reqBufLen = 8
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// UnsetMinimized : request opcode 3
func (i *ForeignToplevelHandleV1) UnsetMinimized() error {
	const opcode = 3
	const _reqBufLen = 8
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// Activate : request opcode 4
//
//	seat: the wl_seat performing the action
func (i *ForeignToplevelHandleV1) Activate(seat *client.Seat) error {
	const opcode = 4
	const _reqBufLen = 8 + 4
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	client.PutUint32(_reqBuf[l:l+4], seat.ID())
	l += 4
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// Close : request opcode 5
func (i *ForeignToplevelHandleV1) Close() error {
	const opcode = 5
	const _reqBufLen = 8
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// Destroy : request opcode 7 (destructor)
func (i *ForeignToplevelHandleV1) Destroy() error {
	defer i.Context().Unregister(i)
	const opcode = 7
	const _reqBufLen = 8
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// SetFullscreen : request opcode 8 (since v2)
//
//	output: the wl_output to fullscreen on, or nil for compositor choice
func (i *ForeignToplevelHandleV1) SetFullscreen(output *client.Output) error {
	const opcode = 8
	const _reqBufLen = 8 + 4
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	var outputID uint32
	if output != nil {
		outputID = output.ID()
	}
	client.PutUint32(_reqBuf[l:l+4], outputID)
	l += 4
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// UnsetFullscreen : request opcode 9 (since v2)
func (i *ForeignToplevelHandleV1) UnsetFullscreen() error {
	const opcode = 9
	const _reqBufLen = 8
	var _reqBuf [_reqBufLen]byte
	l := 0
	client.PutUint32(_reqBuf[l:4], i.ID())
	l += 4
	client.PutUint32(_reqBuf[l:l+4], uint32(_reqBufLen<<16|opcode&0x0000ffff))
	l += 4
	return i.Context().WriteMsg(_reqBuf[:], nil)
}

// --- Events ---

// ForeignToplevelHandleV1TitleEvent : title change (event opcode 0)
type ForeignToplevelHandleV1TitleEvent struct {
	Title string
}
type ForeignToplevelHandleV1TitleHandlerFunc func(ForeignToplevelHandleV1TitleEvent)

func (i *ForeignToplevelHandleV1) SetTitleHandler(f ForeignToplevelHandleV1TitleHandlerFunc) {
	i.titleHandler = f
}

// ForeignToplevelHandleV1AppIdEvent : app-id change (event opcode 1)
type ForeignToplevelHandleV1AppIdEvent struct {
	AppId string
}
type ForeignToplevelHandleV1AppIdHandlerFunc func(ForeignToplevelHandleV1AppIdEvent)

func (i *ForeignToplevelHandleV1) SetAppIdHandler(f ForeignToplevelHandleV1AppIdHandlerFunc) {
	i.appIdHandler = f
}

// ForeignToplevelHandleV1OutputEnterEvent : toplevel entered an output (event opcode 2)
type ForeignToplevelHandleV1OutputEnterEvent struct {
	Output *client.Output
}
type ForeignToplevelHandleV1OutputEnterHandlerFunc func(ForeignToplevelHandleV1OutputEnterEvent)

func (i *ForeignToplevelHandleV1) SetOutputEnterHandler(f ForeignToplevelHandleV1OutputEnterHandlerFunc) {
	i.outputEnterHandler = f
}

// ForeignToplevelHandleV1OutputLeaveEvent : toplevel left an output (event opcode 3)
type ForeignToplevelHandleV1OutputLeaveEvent struct {
	Output *client.Output
}
type ForeignToplevelHandleV1OutputLeaveHandlerFunc func(ForeignToplevelHandleV1OutputLeaveEvent)

func (i *ForeignToplevelHandleV1) SetOutputLeaveHandler(f ForeignToplevelHandleV1OutputLeaveHandlerFunc) {
	i.outputLeaveHandler = f
}

// ForeignToplevelHandleV1StateEvent : the toplevel state changed (event opcode 4)
type ForeignToplevelHandleV1StateEvent struct {
	State []ToplevelState
}
type ForeignToplevelHandleV1StateHandlerFunc func(ForeignToplevelHandleV1StateEvent)

func (i *ForeignToplevelHandleV1) SetStateHandler(f ForeignToplevelHandleV1StateHandlerFunc) {
	i.stateHandler = f
}

// ForeignToplevelHandleV1DoneEvent : all information has been sent (event opcode 5)
type ForeignToplevelHandleV1DoneEvent struct{}
type ForeignToplevelHandleV1DoneHandlerFunc func(ForeignToplevelHandleV1DoneEvent)

func (i *ForeignToplevelHandleV1) SetDoneHandler(f ForeignToplevelHandleV1DoneHandlerFunc) {
	i.doneHandler = f
}

// ForeignToplevelHandleV1ClosedEvent : this toplevel has been destroyed (event opcode 6)
type ForeignToplevelHandleV1ClosedEvent struct{}
type ForeignToplevelHandleV1ClosedHandlerFunc func(ForeignToplevelHandleV1ClosedEvent)

func (i *ForeignToplevelHandleV1) SetClosedHandler(f ForeignToplevelHandleV1ClosedHandlerFunc) {
	i.closedHandler = f
}

// ForeignToplevelHandleV1ParentEvent : parent change (event opcode 7, since v3)
type ForeignToplevelHandleV1ParentEvent struct {
	Parent *ForeignToplevelHandleV1 // nil if no parent
}
type ForeignToplevelHandleV1ParentHandlerFunc func(ForeignToplevelHandleV1ParentEvent)

func (i *ForeignToplevelHandleV1) SetParentHandler(f ForeignToplevelHandleV1ParentHandlerFunc) {
	i.parentHandler = f
}

func (i *ForeignToplevelHandleV1) Dispatch(opcode uint32, fd int, data []byte) {
	switch opcode {
	case 0: // title
		if i.titleHandler == nil {
			return
		}
		var e ForeignToplevelHandleV1TitleEvent
		l := 0
		titleLen := client.PaddedLen(int(client.Uint32(data[l : l+4])))
		l += 4
		e.Title = client.String(data[l : l+titleLen])
		l += titleLen
		i.titleHandler(e)

	case 1: // app_id
		if i.appIdHandler == nil {
			return
		}
		var e ForeignToplevelHandleV1AppIdEvent
		l := 0
		appIdLen := client.PaddedLen(int(client.Uint32(data[l : l+4])))
		l += 4
		e.AppId = client.String(data[l : l+appIdLen])
		l += appIdLen
		i.appIdHandler(e)

	case 2: // output_enter
		if i.outputEnterHandler == nil {
			return
		}
		var e ForeignToplevelHandleV1OutputEnterEvent
		l := 0
		outputID := client.Uint32(data[l : l+4])
		l += 4
		if p := i.Context().GetProxy(outputID); p != nil {
			e.Output, _ = p.(*client.Output)
		}
		i.outputEnterHandler(e)

	case 3: // output_leave
		if i.outputLeaveHandler == nil {
			return
		}
		var e ForeignToplevelHandleV1OutputLeaveEvent
		l := 0
		outputID := client.Uint32(data[l : l+4])
		l += 4
		if p := i.Context().GetProxy(outputID); p != nil {
			e.Output, _ = p.(*client.Output)
		}
		i.outputLeaveHandler(e)

	case 4: // state (array of uint32 state values)
		if i.stateHandler == nil {
			return
		}
		var e ForeignToplevelHandleV1StateEvent
		l := 0
		arrayLen := int(client.Uint32(data[l : l+4]))
		l += 4
		numStates := arrayLen / 4
		e.State = make([]ToplevelState, numStates)
		for j := 0; j < numStates; j++ {
			e.State[j] = ToplevelState(client.Uint32(data[l : l+4]))
			l += 4
		}
		i.stateHandler(e)

	case 5: // done
		if i.doneHandler == nil {
			return
		}
		i.doneHandler(ForeignToplevelHandleV1DoneEvent{})

	case 6: // closed
		if i.closedHandler == nil {
			return
		}
		i.closedHandler(ForeignToplevelHandleV1ClosedEvent{})

	case 7: // parent (since v3, nullable)
		if i.parentHandler == nil {
			return
		}
		var e ForeignToplevelHandleV1ParentEvent
		l := 0
		parentID := client.Uint32(data[l : l+4])
		l += 4
		if parentID != 0 {
			if p := i.Context().GetProxy(parentID); p != nil {
				e.Parent, _ = p.(*ForeignToplevelHandleV1)
			}
		}
		i.parentHandler(e)
	}
}
