package mcinstall

import (
	"github.com/danielpaulus/go-ios/ios"
	"github.com/google/uuid"
)

// singleAppModeProfileIdentifier is the stable outer PayloadIdentifier used
// for the profile StartSingleAppMode installs, so StopSingleAppMode can
// reliably remove it again by identifier regardless of when it was
// installed. Distinct from Apple Configurator's own
// "com.apple.configurator.singleappmode" so the two never collide.
const singleAppModeProfileIdentifier = "com.qawolf.singleappmode"

// SingleAppModeOptions mirrors the "Options" dictionary of the
// com.apple.app.lock configuration profile payload. All fields default to
// false (Apple's own defaults) if left unset.
type SingleAppModeOptions struct {
	// DisableTouch disables the touch screen entirely.
	DisableTouch bool
	// DisableAutoLock keeps the device from sleeping while locked to the app.
	DisableAutoLock      bool
	EnableVoiceOver      bool
	EnableZoom           bool
	EnableInvertColors   bool
	EnableAssistiveTouch bool
	EnableSpeakSelection bool
}

// buildSingleAppModeProfile constructs a com.apple.app.lock configuration
// profile locking a supervised device to bundleID until the profile is
// removed (Apple's device-management schema: "locks to the specified app
// until removal of the profile" and "returns to the app automatically upon
// wake or restart"). PayloadRemovalDisallowed is deliberately omitted/false
// so StopSingleAppMode can always remove it again.
func buildSingleAppModeProfile(bundleID string, opts SingleAppModeOptions) []byte {
	payload := map[string]interface{}{
		"PayloadType":        "com.apple.app.lock",
		"PayloadIdentifier":  singleAppModeProfileIdentifier + ".payload",
		"PayloadUUID":        uuid.New().String(),
		"PayloadVersion":     1,
		"PayloadDisplayName": "Single App Mode",
		"App": map[string]interface{}{
			"Identifier": bundleID,
		},
		"Options": map[string]interface{}{
			"DisableTouch":         opts.DisableTouch,
			"DisableAutoLock":      opts.DisableAutoLock,
			"EnableVoiceOver":      opts.EnableVoiceOver,
			"EnableZoom":           opts.EnableZoom,
			"EnableInvertColors":   opts.EnableInvertColors,
			"EnableAssistiveTouch": opts.EnableAssistiveTouch,
			"EnableSpeakSelection": opts.EnableSpeakSelection,
		},
	}

	config := map[string]interface{}{
		"PayloadContent":     []interface{}{payload},
		"PayloadDisplayName": "QA Wolf Single App Mode",
		"PayloadIdentifier":  singleAppModeProfileIdentifier,
		"PayloadType":        "Configuration",
		"PayloadUUID":        uuid.New().String(),
		"PayloadVersion":     1,
	}

	return ios.ToPlistBytes(config)
}

// StartSingleAppMode locks a supervised device to bundleID by installing a
// com.apple.app.lock profile (silent install, requires the supervision
// identity). The lock persists across sleep/wake and restart until
// StopSingleAppMode removes it.
func (mcInstallConn *Connection) StartSingleAppMode(bundleID string, opts SingleAppModeOptions, p12bytes []byte, p12Password string) error {
	profile := buildSingleAppModeProfile(bundleID, opts)
	return mcInstallConn.AddProfileSupervised(profile, p12bytes, p12Password)
}

// StopSingleAppMode removes the profile StartSingleAppMode installed,
// releasing the device from the app lock.
func (mcInstallConn *Connection) StopSingleAppMode() error {
	return mcInstallConn.RemoveProfile(singleAppModeProfileIdentifier)
}
