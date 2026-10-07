package main

import (
	"fmt"
	"log/slog"

	"github.com/danielpaulus/go-ios/ios/hid"
)

var hidButtons = map[string]hid.Button{
	"home": hid.ButtonHome,
	"lock": hid.ButtonLock,
}

func runHIDCommand(ctx commandContext) {
	if !ctx.Device.SupportsRsd() {
		exitIfError("hid command requires iOS 27+ with tunnel", fmt.Errorf("tunnel not running. Start with: ios tunnel start"))
	}

	var press func(*hid.Session) error
	if button, _ := ctx.Args.Bool("button"); button {
		name, _ := ctx.Args.String("<button>")
		b, ok := hidButtons[name]
		if !ok {
			exitIfError("hid button", fmt.Errorf("unknown button %q, use home or lock", name))
		}
		press = func(s *hid.Session) error { return s.PressButton(b) }
	}
	if typeText, _ := ctx.Args.Bool("type"); typeText {
		text, _ := ctx.Args.String("<text>")
		press = func(s *hid.Session) error { return s.Type(text) }
	}

	session, err := hid.NewSession(ctx.Device)
	exitIfError("hid: failed to connect to the HID service", err)
	err = press(session)
	if closeErr := session.Close(); closeErr != nil {
		slog.Error("Failed to close the HID session", "error", closeErr)
	}
	exitIfError("hid: failed to send input", err)
}
