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
	// _IOWR('E', 0x20, 8) and _IOWR('E', 0x21, 96): EVIOCGBIT for event
	// types and key codes.
	eviocgbitType = 0xC0084520
	eviocgbitKey  = 0xC0604521
)

// Monitor observes evdev devices and fires registered commands when a
// modifier is pressed and released alone. Device watching starts lazily on
// the first SetBinds call and readers run for the daemon's lifetime; binds
// can be swapped at any time.
type Monitor struct {
	mu     sync.Mutex
	binds  map[string]string
	open   bool
	warned bool
}

func NewMonitor() *Monitor {
	return &Monitor{binds: map[string]string{}}
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
	opened := m.openDevices()
	m.mu.Lock()
	defer m.mu.Unlock()
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
// goroutine per device. It reports whether at least one device was opened.
func (m *Monitor) openDevices() bool {
	matches, err := filepath.Glob("/dev/input/event*")
	if err != nil {
		return false
	}
	opened := false
	for _, path := range matches {
		fd, err := unix.Open(path, unix.O_RDONLY, 0)
		if err != nil {
			continue
		}
		if !isKeyboard(uintptr(fd)) {
			unix.Close(fd)
			continue
		}
		opened = true
		go m.readLoop(path, fd)
	}
	return opened
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
