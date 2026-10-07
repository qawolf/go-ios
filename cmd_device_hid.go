package main

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/danielpaulus/go-ios/ios"
	"github.com/danielpaulus/go-ios/ios/deviceinfo"
	"github.com/danielpaulus/go-ios/ios/display"
	"github.com/danielpaulus/go-ios/ios/hid"
	"github.com/docopt/docopt-go"
)

var hidButtons = map[string]hid.Button{
	"home": hid.ButtonHome,
	"lock": hid.ButtonLock,
}

const (
	hidTapHold             = 60 * time.Millisecond
	hidWakeSettle          = 1500 * time.Millisecond
	hidDragStep            = 15 * time.Millisecond
	hidDefaultDragDuration = 300 * time.Millisecond
)

func runHIDCommand(ctx commandContext) {
	if !ctx.Device.SupportsRsd() {
		exitIfError("hid command requires iOS 27+ with tunnel", fmt.Errorf("tunnel not running. Start with: ios tunnel start"))
	}
	input, err := hidInputFromArgs(ctx.Args)
	exitIfError("hid", err)

	stopStream := func() {}
	if wake, _ := ctx.Args.Bool("--wake"); wake {
		stopStream, err = startWakeStream(ctx.Device)
		exitIfError("hid --wake: failed to start a display stream", err)
	}

	session, err := hid.NewSession(ctx.Device)
	if err == nil {
		err = input(session)
		if closeErr := session.Close(); closeErr != nil {
			slog.Error("Failed to close the HID session", "error", closeErr)
		}
	}
	stopStream()
	exitIfError("hid: failed to send input", err)
}

// hidInputFromArgs checks the arguments before anything connects and returns
// the input to send.
func hidInputFromArgs(args docopt.Opts) (func(*hid.Session) error, error) {
	switch {
	case boolArg(args, "button"):
		name, _ := args.String("<button>")
		b, ok := hidButtons[name]
		if !ok {
			return nil, fmt.Errorf("unknown button %q, use home or lock", name)
		}
		return func(s *hid.Session) error { return s.PressButton(b) }, nil

	case boolArg(args, "type"):
		text, _ := args.String("<text>")
		return func(s *hid.Session) error { return s.Type(text) }, nil

	case boolArg(args, "tap"):
		p, err := hidPoint(args, "<x>", "<y>")
		if err != nil {
			return nil, err
		}
		return func(s *hid.Session) error {
			if err := s.TouchDown(p); err != nil {
				return err
			}
			time.Sleep(hidTapHold)
			return s.TouchUp(p)
		}, nil

	case boolArg(args, "drag"):
		from, err := hidPoint(args, "<x1>", "<y1>")
		if err != nil {
			return nil, err
		}
		to, err := hidPoint(args, "<x2>", "<y2>")
		if err != nil {
			return nil, err
		}
		duration := hidDefaultDragDuration
		if raw, _ := args.String("--duration"); raw != "" {
			seconds, err := strconv.ParseFloat(raw, 64)
			if err != nil || seconds <= 0 {
				return nil, fmt.Errorf("--duration must be a positive number of seconds, got %q", raw)
			}
			duration = time.Duration(seconds * float64(time.Second))
		}
		return func(s *hid.Session) error { return hidDrag(s, from, to, duration) }, nil
	}
	return nil, fmt.Errorf("unknown hid command")
}

// hidPoint reads a position as two integers from 0 to 65535, independent of the
// screen's pixel size and orientation.
func hidPoint(args docopt.Opts, xKey, yKey string) (hid.Point, error) {
	var coords [2]uint16
	for i, key := range []string{xKey, yKey} {
		raw, _ := args.String(key)
		v, err := strconv.ParseUint(raw, 10, 16)
		if err != nil {
			return hid.Point{}, fmt.Errorf("%s must be an integer from 0 to 65535, got %q", key, raw)
		}
		coords[i] = uint16(v)
	}
	return hid.Point{X: coords[0], Y: coords[1]}, nil
}

// hidDrag moves one contact along hidDragPoints, then lifts it.
func hidDrag(s *hid.Session, from, to hid.Point, duration time.Duration) error {
	for _, p := range hidDragPoints(from, to, duration) {
		if err := s.TouchDown(p); err != nil {
			return err
		}
		time.Sleep(hidDragStep)
	}
	return s.TouchUp(to)
}

// hidDragPoints spaces the contacts of a drag hidDragStep apart over duration,
// from start to end inclusive.
func hidDragPoints(from, to hid.Point, duration time.Duration) []hid.Point {
	steps := int(duration / hidDragStep)
	if steps < 1 {
		steps = 1
	}
	points := make([]hid.Point, 0, steps+1)
	for i := 0; i <= steps; i++ {
		t := float64(i) / float64(steps)
		points = append(points, hid.Point{
			X: uint16(float64(from.X) + (float64(to.X)-float64(from.X))*t),
			Y: uint16(float64(from.Y) + (float64(to.Y)-float64(from.Y))*t),
		})
	}
	return points
}

// startWakeStream starts a video stream for the length of the command. The
// stream turns the screen on, which a button press does not do once the lock
// screen has dimmed. It needs a kernel tunnel, because the RTP has to reach a
// receiver on this host.
func startWakeStream(device ios.DeviceEntry) (func(), error) {
	recv, err := display.OpenReceiver(device)
	if err != nil {
		return nil, err
	}
	go func() {
		buf := make([]byte, 65536)
		for {
			if _, err := recv.Read(buf); err != nil {
				return
			}
		}
	}()

	svc, err := display.New(device)
	if err != nil {
		_ = recv.Close()
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	started := time.Now()
	sessionID, err := svc.StartVideoStream(ctx, display.VideoStreamRequest{
		ReceiverIP:   recv.IP(),
		ReceiverPort: recv.Port(),
		SenderIP:     device.Address,
	})
	if err != nil {
		_ = svc.Close()
		_ = recv.Close()
		return nil, err
	}
	// Input sent before the backlight is on is dropped: the device ignores
	// digitizer events while the display is off. The screen came on about a
	// second after the stream started. deviceinfo can still report "activeOn"
	// for a screen that is off, so the wait has a floor as well.
	if err := waitForBacklight(device, 5*time.Second); err != nil {
		slog.Warn("The display stream started, but the screen did not report on", "error", err)
	}
	if wait := hidWakeSettle - time.Since(started); wait > 0 {
		time.Sleep(wait)
	}

	return func() {
		// The stop has to be the only request awaiting a reply on its connection.
		_ = svc.Close()
		stopSvc, err := display.New(device)
		if err == nil {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			if err := stopSvc.StopMediaStream(ctx, sessionID); err != nil {
				slog.Warn("Failed to stop the display stream", "error", err)
			}
			cancel()
			_ = stopSvc.Close()
		}
		_ = recv.Close()
	}, nil
}

// waitForBacklight polls the device's display info until the backlight is no
// longer off. Starting the stream only asks for the wake; it takes about a second.
func waitForBacklight(device ios.DeviceEntry, timeout time.Duration) error {
	info, err := deviceinfo.NewDeviceInfo(device)
	if err != nil {
		return err
	}
	defer func() { _ = info.Close() }()

	deadline := time.Now().Add(timeout)
	for {
		display, err := info.GetDisplayInfo()
		if err != nil {
			return err
		}
		if state, _ := display["backlightState"].(string); state != "" && state != "off" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("backlight still off after %s", timeout)
		}
		time.Sleep(200 * time.Millisecond)
	}
}
