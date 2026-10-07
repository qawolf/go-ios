package main

import (
	"testing"
	"time"

	"github.com/danielpaulus/go-ios/ios/hid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHIDInputFromArgsAcceptsEachCommand(t *testing.T) {
	for _, argv := range [][]string{
		{"hid", "button", "home"},
		{"hid", "button", "lock", "--wake"},
		{"hid", "type", "Hello!"},
		{"hid", "tap", "0", "65535"},
		{"hid", "drag", "32767", "64000", "32767", "20000", "--duration=0.5"},
	} {
		_, err := hidInputFromArgs(parseCLIArgs(t, argv...))
		assert.NoError(t, err, "%v", argv)
	}
}

func TestHIDInputFromArgsRejectsBadValues(t *testing.T) {
	for _, argv := range [][]string{
		{"hid", "button", "volumeup"},
		{"hid", "tap", "x", "10"},
		{"hid", "tap", "65536", "10"},
		{"hid", "tap", "0.5", "10"},
		{"hid", "drag", "0", "0", "10", "10", "--duration=0"},
		{"hid", "drag", "0", "0", "10", "10", "--duration=soon"},
	} {
		_, err := hidInputFromArgs(parseCLIArgs(t, argv...))
		assert.Error(t, err, "%v", argv)
	}
}

func TestHIDDragPointsRunFromStartToEnd(t *testing.T) {
	from, to := hid.Point{X: 100, Y: 60000}, hid.Point{X: 100, Y: 20000}

	points := hidDragPoints(from, to, 300*time.Millisecond)

	require.Len(t, points, 21)
	assert.Equal(t, from, points[0])
	assert.Equal(t, to, points[len(points)-1])
	for i := 1; i < len(points); i++ {
		assert.Less(t, points[i].Y, points[i-1].Y, "the contact moves up every step")
	}
}

func TestHIDDragPointsTakeAtLeastOneStep(t *testing.T) {
	points := hidDragPoints(hid.Point{}, hid.Point{X: 10, Y: 10}, time.Millisecond)
	assert.Equal(t, []hid.Point{{}, {X: 10, Y: 10}}, points)
}
