package hid

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubSleep records the waits instead of sleeping, and restores sleep after the test.
func stubSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var slept []time.Duration
	sleep = func(d time.Duration) { slept = append(slept, d) }
	t.Cleanup(func() { sleep = time.Sleep })
	return &slept
}

func keyReports(reports []report) [][]uint8 {
	var out [][]uint8
	for _, r := range reports {
		if r.kind == "key" {
			out = append(out, r.usages)
		}
	}
	return out
}

func TestBuildKeyboardReport(t *testing.T) {
	got := buildKeyboardReport([]uint8{keyLeftShift, 0x04}, goldenTimestamp)

	want := make([]byte, keyboardReportLen)
	want[0] = reportIDKeyboard
	want[1+0x04/8] = 1 << (0x04 % 8)                 // 'a'
	want[1+keyLeftShift/8] = 1 << (keyLeftShift % 8) // Left Shift
	copy(want[31:37], []byte{0xBC, 0x9A, 0x78, 0x56, 0x34, 0x12})

	if !bytes.Equal(got, want) {
		t.Errorf("report mismatch\n got %x\nwant %x", got, want)
	}
}

func TestEmptyKeyboardReportReleasesEveryKey(t *testing.T) {
	got := buildKeyboardReport(nil, 0)
	require.Len(t, got, keyboardReportLen)
	assert.Equal(t, make([]byte, keyboardBitmapLen), got[keyboardBitmapOffset:keyboardBitmapOffset+keyboardBitmapLen])
}

func TestKeyboardReportIgnoresUsagesPastTheBitmap(t *testing.T) {
	got := buildKeyboardReport([]uint8{0xF0}, 0)
	assert.Equal(t, make([]byte, keyboardBitmapLen), got[keyboardBitmapOffset:keyboardBitmapOffset+keyboardBitmapLen])
}

func TestKeyForRune(t *testing.T) {
	tests := []struct {
		ch   rune
		want key
	}{
		{'a', key{usage: 0x04}},
		{'z', key{usage: 0x1D}},
		{'A', key{usage: 0x04, shift: true}},
		{'1', key{usage: 0x1E}},
		{'0', key{usage: 0x27}},
		{'!', key{usage: 0x1E, shift: true}},
		{')', key{usage: 0x27, shift: true}},
		{' ', key{usage: keySpace}},
		{'\n', key{usage: keyEnter}},
		{'?', key{usage: 0x38, shift: true}},
		{'"', key{usage: 0x34, shift: true}},
	}
	for _, tt := range tests {
		got, ok := keyForRune(tt.ch)
		if assert.True(t, ok, "%q", tt.ch) {
			assert.Equal(t, tt.want, got, "%q", tt.ch)
		}
	}

	_, ok := keyForRune('ç')
	assert.False(t, ok)
}

// The device drops a Shift sent in the same report as the key it modifies.
func TestKeystrokesSendShiftInItsOwnReport(t *testing.T) {
	assert.Equal(t, [][]uint8{{0x04}, {}}, keystrokes(key{usage: 0x04}))
	assert.Equal(t,
		[][]uint8{{keyLeftShift}, {keyLeftShift, 0x04}, {keyLeftShift}, {}},
		keystrokes(key{usage: 0x04, shift: true}))
}

func TestCreateKeyboardPayload(t *testing.T) {
	body := roundTrip(t, buildCreateKeyboardPayload(keyboardServiceID))

	assert.Equal(t, universalFeatureIdentifier, body["featureIdentifier"])
	assert.Equal(t, "Request", body["messageType"])

	service := dict(t, dict(t, dict(t, body, "payload"), "createService"), "_0")
	assert.Equal(t, keyboardServiceID, service["_ServiceID"])
	assert.Equal(t, uint64(6), service["PrimaryUsage"])
	assert.Equal(t, uint64(1), service["PrimaryUsagePage"])

	storage := dict(t, service, "_CoreDevice_codablePropertyStorage")
	assert.Equal(t, keyboardServiceID, dict(t, storage, "_ServiceID")["uint"])
	assert.Equal(t, keyboardReportDescriptor, dict(t, storage, "ReportDescriptor")["data"])
}

func TestTypeRegistersTheKeyboardOnce(t *testing.T) {
	stubSleep(t)
	session, fake := openSession()

	require.NoError(t, session.Type("aB"))
	require.NoError(t, session.Type("c"))

	creates := 0
	for _, r := range fake.reports {
		if r.kind == "createKeyboard" {
			creates++
		}
	}
	assert.Equal(t, 1, creates)
	assert.Equal(t, [][]uint8{
		{0x04}, {},
		{keyLeftShift}, {keyLeftShift, 0x05}, {keyLeftShift}, {},
		{0x06}, {},
	}, keyReports(fake.reports))
}

func TestTypeRejectsAnUnsupportedCharacterBeforeSending(t *testing.T) {
	stubSleep(t)
	session, fake := openSession()

	assert.Error(t, session.Type("aç"))
	assert.Empty(t, fake.reports)
}

func TestTypeReleasesKeysWhenASendFails(t *testing.T) {
	stubSleep(t)
	session, fake := openSession()
	fake.failAt = 2 // Shift went down, Shift+A fails

	assert.Error(t, session.Type("A"))
	assert.Equal(t, [][]uint8{{keyLeftShift}, {}}, keyReports(fake.reports))
}

func TestCloseReleasesAKeyLeftDown(t *testing.T) {
	session, fake := openSession()
	session.keyboardID = keyboardServiceID
	session.keysDown = true

	require.NoError(t, session.Close())
	assert.Equal(t, [][]uint8{{}}, keyReports(fake.reports))
}
