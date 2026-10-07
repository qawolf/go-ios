package hid

import "time"

// keyboardServiceID is the _ServiceID asked for when registering the virtual
// keyboard. dtuhidd answers with the id it settled on, which so far has been
// this one.
const keyboardServiceID uint64 = 0x100002001

const reportIDKeyboard = 0x01

// The device rejects a keyboard report of any other size.
const keyboardReportLen = 39

// Pressed usages are a bitmap over usages 0 to 239 in bytes 1 to 30.
const (
	keyboardBitmapOffset = 1
	keyboardBitmapLen    = 30
	keyboardTimestampAt  = 31
)

// keyboardSettle is the wait between registering the keyboard and the first
// report, and keystrokeInterval the wait between reports. Both are what the
// device tests used.
const (
	keyboardSettle    = 500 * time.Millisecond
	keystrokeInterval = 35 * time.Millisecond
)

// Usages on the HID Keyboard/Keypad page.
const (
	keyEnter     uint8 = 0x28
	keyTab       uint8 = 0x2B
	keySpace     uint8 = 0x2C
	keyLeftShift uint8 = 0xE1
)

// keyboardReportDescriptor declares a boot-style Generic Desktop keyboard, so
// backboardd treats the service as one.
var keyboardReportDescriptor = []byte{
	0x05, 0x01, 0x09, 0x06, 0xA1, 0x01, 0x05, 0x07, 0x19, 0xE0, 0x29, 0xE7, 0x15, 0x00, 0x25, 0x01,
	0x95, 0x08, 0x75, 0x01, 0x81, 0x02, 0x95, 0x01, 0x75, 0x08, 0x81, 0x01, 0x05, 0x07, 0x19, 0x00,
	0x29, 0xFF, 0x15, 0x00, 0x26, 0xFF, 0x00, 0x95, 0x06, 0x75, 0x08, 0x81, 0x00, 0x05, 0x08, 0x19,
	0x01, 0x29, 0x05, 0x15, 0x00, 0x25, 0x01, 0x95, 0x05, 0x75, 0x01, 0x91, 0x02, 0x95, 0x01, 0x75,
	0x03, 0x91, 0x01, 0xC0,
}

func buildCreateKeyboardPayload(serviceID uint64) map[string]interface{} {
	const (
		usagePageGenericDesktop int64 = 1
		usageKeyboard           int64 = 6
		vendorID                int64 = 0x05AC
		productID               int64 = 0x0250
		product                       = "go-ios virtual keyboard"
	)
	// The same properties again in Swift-Codable form, which dtuhidd reads.
	storage := map[string]interface{}{
		"Manufacturer":     map[string]interface{}{"string": "go-ios"},
		"Product":          map[string]interface{}{"string": product},
		"ProductID":        map[string]interface{}{"int": productID},
		"VendorID":         map[string]interface{}{"int": vendorID},
		"PrimaryUsage":     map[string]interface{}{"int": usageKeyboard},
		"PrimaryUsagePage": map[string]interface{}{"int": usagePageGenericDesktop},
		"DeviceUsagePairs": map[string]interface{}{"array": []interface{}{
			map[string]interface{}{"dictionary": map[string]interface{}{
				"DeviceUsage":     map[string]interface{}{"int": usageKeyboard},
				"DeviceUsagePage": map[string]interface{}{"int": usagePageGenericDesktop},
			}},
		}},
		"Transport":                      map[string]interface{}{"string": "USB"},
		"ReportDescriptor":               map[string]interface{}{"data": keyboardReportDescriptor},
		"UniversalControlVirtualService": map[string]interface{}{"bool": true},
		"_ServiceID":                     map[string]interface{}{"uint": serviceID},
	}
	return map[string]interface{}{
		"featureIdentifier": universalFeatureIdentifier,
		"messageType":       "Request",
		"payload": map[string]interface{}{
			"createService": map[string]interface{}{
				"_0": map[string]interface{}{
					"DeviceUsagePairs": []interface{}{
						map[string]interface{}{"DeviceUsage": usageKeyboard, "DeviceUsagePage": usagePageGenericDesktop},
					},
					"PrimaryUsage":                       uint64(usageKeyboard),
					"PrimaryUsagePage":                   uint64(usagePageGenericDesktop),
					"Product":                            product,
					"ProductID":                          productID,
					"VendorID":                           vendorID,
					"_CoreDevice_codablePropertyStorage": storage,
					"_ServiceID":                         serviceID,
				},
			},
		},
	}
}

// buildKeyboardReport carries the full set of usages held down, not a delta: an
// empty set releases every key.
// Layout: [0]=report ID, [1:31]=bitmap of usages 0-239, [31:37]=timestamp,
// [37:39] reserved.
func buildKeyboardReport(usages []uint8, ts uint64) []byte {
	report := make([]byte, keyboardReportLen)
	report[0] = reportIDKeyboard
	for _, u := range usages {
		if int(u) >= keyboardBitmapLen*8 {
			continue
		}
		report[keyboardBitmapOffset+int(u)/8] |= 1 << (u % 8)
	}
	putTimestamp(report[keyboardTimestampAt:keyboardTimestampAt+6], ts)
	return report
}

type key struct {
	usage uint8
	shift bool
}

// usKeys maps the printable ASCII characters, Enter and Tab to keys on a US layout.
var usKeys = func() map[rune]key {
	m := map[rune]key{'\n': {usage: keyEnter}, '\t': {usage: keyTab}, ' ': {usage: keySpace}}
	for i := 0; i < 26; i++ {
		m['a'+rune(i)] = key{usage: 0x04 + uint8(i)}
		m['A'+rune(i)] = key{usage: 0x04 + uint8(i), shift: true}
	}
	for i, ch := range "1234567890" {
		m[ch] = key{usage: 0x1E + uint8(i)}
	}
	for i, ch := range "!@#$%^&*()" {
		m[ch] = key{usage: 0x1E + uint8(i), shift: true}
	}
	pairs := []struct {
		plain, shifted rune
		usage          uint8
	}{
		{'-', '_', 0x2D}, {'=', '+', 0x2E}, {'[', '{', 0x2F}, {']', '}', 0x30},
		{'\\', '|', 0x31}, {';', ':', 0x33}, {'\'', '"', 0x34}, {'`', '~', 0x35},
		{',', '<', 0x36}, {'.', '>', 0x37}, {'/', '?', 0x38},
	}
	for _, p := range pairs {
		m[p.plain] = key{usage: p.usage}
		m[p.shifted] = key{usage: p.usage, shift: true}
	}
	return m
}()

func keyForRune(ch rune) (key, bool) {
	k, ok := usKeys[ch]
	return k, ok
}

// keystrokes is the run of reports that types k. Shift goes in a report of its
// own first: with Shift and the key in one report the device drops the Shift.
func keystrokes(k key) [][]uint8 {
	if k.shift {
		return [][]uint8{{keyLeftShift}, {keyLeftShift, k.usage}, {keyLeftShift}, {}}
	}
	return [][]uint8{{k.usage}, {}}
}
