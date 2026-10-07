package hid

import (
	"fmt"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/xpc"
)

const (
	indigoServiceName = "com.apple.coredevice.hid.indigo"

	buttonFeatureIdentifier = "com.apple.coredevice.feature.remote.hid.button"
)

// Button is a hardware button: its HID usage and how long to hold it down.
type Button struct {
	UsagePage uint64
	Usage     uint64
	Hold      time.Duration
}

// Hold times verified on iOS 27.0.1. A side button held for 500 ms starts
// Siri's long press, and SpringBoard then drops the single press.
var (
	ButtonHome = Button{UsagePage: 0x0C, Usage: 0x40, Hold: 50 * time.Millisecond}
	// ButtonLock is the side button: it locks an unlocked device.
	ButtonLock = Button{UsagePage: 0x0C, Usage: 0x30, Hold: 100 * time.Millisecond}
)

// buttonSettle is how long to wait after the last button event before closing
// the connection. dtuhidd handles the messages asynchronously and drops the
// last one when the connection closes right after it.
const buttonSettle = 100 * time.Millisecond

type buttonState uint64

const (
	buttonDown buttonState = 1
	buttonUp   buttonState = 2
)

type indigoConnection struct {
	conn *xpc.Connection
}

// newIndigo connects to the indigo HID service, which takes hardware button
// events. Like universalhidservice, it ships in the Developer Disk Image.
func newIndigo(device ios.DeviceEntry) (*indigoConnection, error) {
	conn, err := ios.ConnectToXpcServiceTunnelIface(device, indigoServiceName)
	if err != nil {
		return nil, fmt.Errorf("newIndigo: %w", err)
	}
	return &indigoConnection{conn: conn}, nil
}

// The device never returns a response, so a nil error means it was sent.
func (c *indigoConnection) sendButton(state buttonState, b Button) error {
	if err := c.conn.Send(buildButtonPayload(state, b.UsagePage, b.Usage), xpc.HeartbeatRequestFlag); err != nil {
		return fmt.Errorf("sendButton: %w", err)
	}
	return nil
}

func (c *indigoConnection) Close() error {
	return c.conn.Close()
}

func buildButtonPayload(state buttonState, usagePage, usage uint64) map[string]interface{} {
	return map[string]interface{}{
		"featureIdentifier": buttonFeatureIdentifier,
		"messageType":       "IndigoButtonEvent",
		"payload": map[string]interface{}{
			"state":     uint64(state),
			"usagePage": usagePage,
			"usageCode": usage,
		},
	}
}
