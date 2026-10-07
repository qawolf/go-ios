package hid

import (
	"fmt"
	"sync"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/golog"
)

const logModule = "go-ios/hid"

// Point is a position on the touchscreen, 0 to 65535 on each axis, independent
// of pixel size and orientation.
type Point struct {
	X uint16
	Y uint16
}

// hidConn is the part of the HID connection this package uses, extracted so
// gestures can be exercised without a device.
type hidConn interface {
	sendTouch(state touchState, p Point) error
	createKeyboard() (uint64, error)
	sendKeyboard(serviceID uint64, usages []uint8) error
	Close() error
}

// buttonConn is the indigo connection, which carries hardware button events.
type buttonConn interface {
	sendButton(state buttonState, b Button) error
	Close() error
}

// sleep is swapped out in tests, so the timings below cost them nothing.
var sleep = time.Sleep

// Session delivers touch, button and keyboard input to one device.
//
// A media stream has to be running for any of this to reach the screen. Without
// one the device accepts every report and discards it, returning no error, so a
// caller that forgets sees silence rather than a failure. Starting that stream
// and keeping it up for the session's lifetime is the caller's job: ios/display
// does it, and this package deliberately does not.
type Session struct {
	device ios.DeviceEntry

	mutex sync.Mutex
	hid   hidConn

	// Tracks a contact held by the Touch* methods, so closing mid-gesture lifts it
	// rather than leaving the device believing a finger is down.
	contactDown bool
	lastContact Point

	// buttons is opened by the first PressButton; lastButton is when it last
	// sent, so Close can let dtuhidd take that message before closing.
	buttons    buttonConn
	lastButton time.Time

	// keyboardID is set once Type has registered the virtual keyboard.
	keyboardID uint64
	keysDown   bool

	closed bool
}

// NewSession connects to the device's HID service. iOS 27+ and a kernel tunnel.
//
// Touch does nothing until a media stream is running: the device accepts the
// reports and throws them away, returning no error. Starting that stream and
// keeping it up is the caller's job.
func NewSession(device ios.DeviceEntry) (*Session, error) {
	session := &Session{device: device}

	conn, err := newUniversal(device)
	if err != nil {
		return nil, fmt.Errorf("NewSession: %w", err)
	}
	session.hid = conn
	return session, nil
}

// TouchDown reports a contact at point and leaves it there. Call it again at a
// new point to move: the device tracks where the contact is, not what changed,
// so there is no separate move. TouchUp lifts it.
func (s *Session) TouchDown(point Point) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := s.checkOpen(); err != nil {
		return err
	}
	if err := s.hid.sendTouch(touchContact, point); err != nil {
		return fmt.Errorf("TouchDown: %w", err)
	}
	s.contactDown = true
	s.lastContact = point
	return nil
}

// TouchUp lifts the contact, and is a no-op when nothing is down. It takes no
// context deliberately: a caller tearing down is exactly when the finger has to
// come off the screen, so this must not be skippable.
func (s *Session) TouchUp(point Point) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := s.checkOpen(); err != nil {
		return err
	}
	if !s.contactDown {
		return nil
	}
	if err := s.hid.sendTouch(touchRelease, point); err != nil {
		return fmt.Errorf("TouchUp: %w", err)
	}
	s.contactDown = false
	return nil
}

// Close lifts any held contact, releases any held key and closes the HID
// connections. It is idempotent and waits for a gesture in flight.
func (s *Session) Close() error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true

	// Lift a contact left down by an interrupted input stream first: once the
	// stream is gone the device would keep believing a finger is on the screen.
	if s.contactDown {
		if err := s.hid.sendTouch(touchRelease, s.lastContact); err != nil {
			golog.Warn("failed to lift a held contact while closing, the device may still consider the screen touched",
				"module", logModule, "error", err)
		}
		s.contactDown = false
	}
	if s.hid != nil {
		s.releaseKeys()
	}

	if s.buttons != nil {
		if wait := buttonSettle - time.Since(s.lastButton); wait > 0 {
			sleep(wait)
		}
		if err := s.buttons.Close(); err != nil {
			golog.Warn("failed to close the button connection", "module", logModule, "error", err)
		}
		s.buttons = nil
	}

	if s.hid != nil {
		if err := s.hid.Close(); err != nil {
			return fmt.Errorf("Session.Close: %w", err)
		}
		s.hid = nil
	}
	return nil
}

// PressButton presses b, holds it for b.Hold and releases it. It needs no
// media stream.
func (s *Session) PressButton(b Button) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := s.checkOpen(); err != nil {
		return err
	}
	if s.buttons == nil {
		conn, err := newIndigo(s.device)
		if err != nil {
			return fmt.Errorf("PressButton: %w", err)
		}
		s.buttons = conn
	}
	if err := s.buttons.sendButton(buttonDown, b); err != nil {
		return fmt.Errorf("PressButton: %w", err)
	}
	sleep(b.Hold)
	err := s.buttons.sendButton(buttonUp, b)
	s.lastButton = time.Now()
	if err != nil {
		return fmt.Errorf("PressButton: %w", err)
	}
	return nil
}

// Type types text on a virtual hardware keyboard with a US layout. It checks
// every character before sending anything, so an unsupported one fails the
// whole call. It needs no media stream.
func (s *Session) Type(text string) error {
	keys := make([]key, 0, len(text))
	for _, ch := range text {
		k, ok := keyForRune(ch)
		if !ok {
			return fmt.Errorf("Type: no key for %q", ch)
		}
		keys = append(keys, k)
	}

	s.mutex.Lock()
	defer s.mutex.Unlock()
	if err := s.checkOpen(); err != nil {
		return err
	}
	if s.keyboardID == 0 {
		id, err := s.hid.createKeyboard()
		if err != nil {
			return fmt.Errorf("Type: %w", err)
		}
		s.keyboardID = id
		sleep(keyboardSettle)
	}
	for _, k := range keys {
		for _, usages := range keystrokes(k) {
			if err := s.hid.sendKeyboard(s.keyboardID, usages); err != nil {
				s.releaseKeys()
				return fmt.Errorf("Type: %w", err)
			}
			s.keysDown = len(usages) > 0
			sleep(keystrokeInterval)
		}
	}
	return nil
}

// releaseKeys sends an empty report if a key may still be down, so an
// interrupted Type does not leave the device seeing a held key.
func (s *Session) releaseKeys() {
	if !s.keysDown {
		return
	}
	if err := s.hid.sendKeyboard(s.keyboardID, nil); err != nil {
		golog.Warn("failed to release held keys, the device may still consider a key pressed",
			"module", logModule, "error", err)
	}
	s.keysDown = false
}

func (s *Session) checkOpen() error {
	if s.closed || s.hid == nil {
		return fmt.Errorf("session is closed")
	}
	return nil
}
