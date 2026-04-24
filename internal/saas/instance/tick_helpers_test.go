package instance

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/datatypes"
)

func TestMakeClientOrderID_Format(t *testing.T) {
	got := makeClientOrderID(42, "MACRO", 1700000000000, 0)
	assert.Equal(t, "inst42-MACRO-1700000000000-0", got)
}

func TestMakeClientOrderID_UniqueAcrossSlots(t *testing.T) {
	a := makeClientOrderID(1, "MICRO", 100, 0)
	b := makeClientOrderID(1, "MICRO", 100, 1)
	assert.NotEqual(t, a, b)
}

func TestMakeClientOrderID_UniqueAcrossEngines(t *testing.T) {
	a := makeClientOrderID(1, "MACRO", 100, 0)
	b := makeClientOrderID(1, "MICRO", 100, 0)
	assert.NotEqual(t, a, b)
}

func TestFormatFloat_TrimsTrailingZeros(t *testing.T) {
	assert.Equal(t, "10", formatFloat(10.0))
	assert.Equal(t, "10.5", formatFloat(10.5))
	assert.Equal(t, "0.00001", formatFloat(0.00001))
}

func TestDecodeRuntime_Empty(t *testing.T) {
	rt := decodeRuntime(nil)
	assert.NotNil(t, rt.Extras)
	assert.Equal(t, int64(0), rt.LastProcessedBarTime)
}

func TestDecodeRuntime_ValidJSON(t *testing.T) {
	blob := []byte(`{"LastProcessedBarTime":1700,"Extras":{"foo":1.5}}`)
	rt := decodeRuntime(datatypes.JSON(blob))
	assert.Equal(t, int64(1700), rt.LastProcessedBarTime)
	assert.Equal(t, 1.5, rt.Extras["foo"])
}

func TestDecodeRuntime_CorruptJSONFallsBack(t *testing.T) {
	rt := decodeRuntime(datatypes.JSON([]byte("{not json")))
	// 解码失败也要有安全的空 Extras map
	assert.NotNil(t, rt.Extras)
}

