package hid

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeButtons struct {
	states []buttonState
	closed bool
}

func (f *fakeButtons) sendButton(state buttonState, b Button) error {
	f.states = append(f.states, state)
	return nil
}

func (f *fakeButtons) Close() error {
	f.closed = true
	return nil
}

func TestButtonPayload(t *testing.T) {
	body := roundTrip(t, buildButtonPayload(buttonDown, ButtonHome.UsagePage, ButtonHome.Usage))

	assert.Equal(t, "IndigoButtonEvent", body["messageType"])
	assert.Equal(t, buttonFeatureIdentifier, body["featureIdentifier"])
	payload := dict(t, body, "payload")
	assert.Equal(t, uint64(1), payload["state"])
	assert.Equal(t, uint64(0x0C), payload["usagePage"])
	assert.Equal(t, uint64(0x40), payload["usageCode"])
}

func TestPressButtonHoldsThenReleases(t *testing.T) {
	slept := stubSleep(t)
	buttons := &fakeButtons{}
	session := &Session{hid: &fakeHID{}, buttons: buttons}

	require.NoError(t, session.PressButton(ButtonLock))

	assert.Equal(t, []buttonState{buttonDown, buttonUp}, buttons.states)
	assert.Equal(t, []time.Duration{ButtonLock.Hold}, *slept)
}

// dtuhidd drops the last message when the connection closes right after it.
func TestCloseLetsTheLastButtonEventSettle(t *testing.T) {
	slept := stubSleep(t)
	buttons := &fakeButtons{}
	session := &Session{hid: &fakeHID{}, buttons: buttons}

	require.NoError(t, session.PressButton(ButtonHome))
	require.NoError(t, session.Close())

	require.Len(t, *slept, 2)
	assert.Greater(t, (*slept)[1], time.Duration(0))
	assert.LessOrEqual(t, (*slept)[1], buttonSettle)
	assert.True(t, buttons.closed)
}
