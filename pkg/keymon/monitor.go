package keymon

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// inputEvent mirrors linux struct input_event on 64-bit: timeval (2x int64)
// + u16 type + u16 code + s32 value.
type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
}

const (
	inputEventSize = int(unsafe.Sizeof(inputEvent{}))

	evMax    = 0x1f
	keyMax   = 0x2ff
	evKeyBit = 1
	keyA     = 30
	keyZ     = 48
	// EVIOCGBIT(0, 8) and EVIOCGBIT(EV_KEY, 96), per linux/input.h:
	// _IOC(_IOC_READ, 'E', 0x20 + ev, len) — read direction only.
	eviocgbitType = 0x80084520
	eviocgbitKey  = 0x80604521
)

// Status is a snapshot of the monitor state for introspection.
type Status struct {
	Open    bool              `json:"open"`
	Binds   map[string]string `json:"binds"`
	Devices []string          `json:"devices"`
	Errors  map[string]string `json:"errors"`
}

// Monitor observes evdev devices and fires registered commands when a
// modifier is pressed and released alone. Device watching starts lazily on
// the first SetBinds call and readers run for the daemon's lifetime; binds
// can be swapped at any time.
type Monitor struct {
	mu      sync.Mutex
	binds   map[string]string
	open    bool
	warned  bool
	devices map[string]bool
	errors  map[string]string
}

func NewMonitor() *Monitor {
	return &Monitor{
		binds:   map[string]string{},
		devices: map[string]bool{},
		errors:  map[string]string{},
	}
}

// Status returns the current monitor state for debugging.
func (m *Monitor) Status() Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := Status{
		Open:    m.open,
		Binds:   make(map[string]string, len(m.binds)),
		Devices: make([]string, 0, len(m.devices)),
		Errors:  make(map[string]string, len(m.errors)),
	}
	for k, v := range m.binds {
		st.Binds[k] = v
	}
	for path := range m.devices {
		st.Devices = append(st.Devices, path)
	}
	for k, v := range m.errors {
		st.Errors[k] = v
	}
	return st
}

// SetBinds replaces the modifier-alone commands (group → shell command).
// Pass an empty map to disable. The devices are opened on the first
// non-empty call and retried if they were unreadable before.
func (m *Monitor) SetBinds(binds map[string]string) {
	m.mu.Lock()
	m.binds = make(map[string]string, len(binds))
	for group, cmd := range binds {
		if cmd != "" {
			m.binds[group] = cmd
		}
	}
	needOpen := !m.open && len(m.binds) > 0
	m.mu.Unlock()

	if !needOpen {
		return
	}
	opened, devices, errs := m.openDevices()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors = errs
	for _, path := range devices {
		m.devices[path] = true
	}
	if opened {
		m.open = true
	} else if !m.warned {
		m.warned = true
		fmt.Println("keymon: cannot read any /dev/input device — " +
			"modifier-alone binds are disabled (add the daemon user to the 'input' group)")
	}
}

func (m *Monitor) currentBinds() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[string]string, len(m.binds))
	for k, v := range m.binds {
		out[k] = v
	}
	return out
}

// openDevices opens every keyboard-like evdev device and starts a reader
// goroutine per device. It returns whether at least one device was opened,
// the opened paths, and per-device errors for introspection.
func (m *Monitor) openDevices() (bool, []string, map[string]string) {
	errs := map[string]string{}
	matches, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		errs["glob"] = err.Error()
		return false, nil, errs
	}
	opened := false
	devices := []string{}
	for _, path := range matches {
		fd, err := unix.Open(path, unix.O_RDONLY, 0)
		if err != nil {
			errs[path] = fmt.Sprintf("open: %v", err)
			continue
		}
		if !isKeyboard(uintptr(fd)) {
			unix.Close(fd)
			errs[path] = "not a keyboard (no EV_KEY letters/modifiers)"
			continue
		}
		devices = append(devices, path)
		opened = true
		go m.readLoop(path, fd)
	}
	return opened, devices, errs
}

// isKeyboard reports whether the fd has EV_KEY capability and at least one
// letter key or a tracked modifier.
func isKeyboard(fd uintptr) bool {
	var typeBits [8]byte
	if err := ioctlBit(fd, eviocgbitType, typeBits[:]); err != nil {
		return false
	}
	if typeBits[evKeyBit/8]&(1<<(evKeyBit%8)) == 0 {
		return false
	}
	var keyBits [96]byte
	if err := ioctlBit(fd, eviocgbitKey, keyBits[:]); err != nil {
		return false
	}
	hasLetter := false
	for code := keyA; code <= keyZ; code++ {
		if keyBits[code/8]&(1<<(code%8)) != 0 {
			hasLetter = true
			break
		}
	}
	if hasLetter {
		return true
	}
	for code := range modifierGroups {
		if keyBits[code/8]&(1<<(code%8)) != 0 {
			return true
		}
	}
	return false
}

func ioctlBit(fd uintptr, req uint, buf []byte) error {
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, uintptr(req), uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
		return errno
	}
	return nil
}

// readLoop consumes evdev events from one device until the fd errors out.
// On failure it marks the device gone in the monitor state.
func (m *Monitor) readLoop(path string, fd int) {
	defer unix.Close(fd)
	machine := NewMachine()
	buf := make([]byte, inputEventSize*64)
	for {
		n, err := unix.Read(fd, buf)
		if err != nil {
			if err == syscall.EINTR {
				continue
			}
			m.mu.Lock()
			delete(m.devices, path)
			m.errors[path] = fmt.Sprintf("read: %v", err)
			m.mu.Unlock()
			fmt.Printf("keymon: %s reader stopped: %v\n", path, err)
			return
		}
		for off := 0; off+inputEventSize <= n; off += inputEventSize {
			ev := *(*inputEvent)(unsafe.Pointer(&buf[off]))
			if group := machine.Process(ev.Type, ev.Code, ev.Value); group != "" {
				m.dispatch(group)
			}
		}
	}
}

// dispatch executes the command bound to a fired modifier group.
func (m *Monitor) dispatch(group string) {
	binds := m.currentBinds()
	cmd, ok := binds[group]
	if !ok {
		return
	}
	fmt.Printf("keymon: %s\n", Fired{Group: group, Command: cmd})
	go func() {
		if err := runShell(cmd); err != nil {
			fmt.Printf("keymon: failed to run %q: %v\n", cmd, err)
		}
	}()
}

func runShell(cmd string) error {
	return exec.Command("sh", "-c", cmd).Run()
}
