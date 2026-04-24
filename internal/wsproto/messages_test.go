package wsproto

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Envelope JSON 往返保证 type + payload 正确序列化/反序列化。
func TestEnvelope_TradeCommandRoundtrip(t *testing.T) {
	env := Envelope{
		Type: TypeCommand,
		Payload: CommandPayload{
			TradeCommand: TradeCommand{
				ClientOrderID: "inst1-MACRO-100-0",
				Action:        "BUY",
				Engine:        "MACRO",
				Symbol:        "BTCUSDT",
				AmountUSDT:    "50.5",
				LotType:       "DEAD_STACK",
			},
		},
	}
	blob, err := json.Marshal(env)
	require.NoError(t, err)

	var back Envelope
	require.NoError(t, json.Unmarshal(blob, &back))
	assert.Equal(t, TypeCommand, back.Type)

	// 再次解析 Payload 字段
	payloadBlob, _ := json.Marshal(back.Payload)
	var cp CommandPayload
	require.NoError(t, json.Unmarshal(payloadBlob, &cp))
	assert.Equal(t, "inst1-MACRO-100-0", cp.ClientOrderID)
	assert.Equal(t, "BUY", cp.Action)
	assert.Equal(t, "50.5", cp.AmountUSDT)
}

func TestDeltaReport_InitialSnapshotRoundtrip(t *testing.T) {
	report := DeltaReport{
		Balances: []Balance{
			{Asset: "BTC", Free: "0.5", Locked: "0"},
			{Asset: "USDT", Free: "1000.00", Locked: "0"},
		},
	}
	blob, err := json.Marshal(report)
	require.NoError(t, err)

	var back DeltaReport
	require.NoError(t, json.Unmarshal(blob, &back))
	assert.Empty(t, back.ClientOrderID)
	assert.Nil(t, back.Execution)
	assert.Len(t, back.Balances, 2)
}

func TestDeltaReport_ExecutionRoundtrip(t *testing.T) {
	report := DeltaReport{
		ClientOrderID: "oid-99",
		Execution: &ExecutionDetail{
			ClientOrderID: "oid-99",
			Status:        "FILLED",
			FilledQty:     "0.001",
			FilledPrice:   "50000",
			FilledQuote:   "50.00",
			Fee:           "0.05",
			FeeAsset:      "USDT",
		},
	}
	blob, _ := json.Marshal(report)
	var back DeltaReport
	require.NoError(t, json.Unmarshal(blob, &back))
	assert.Equal(t, "oid-99", back.ClientOrderID)
	assert.NotNil(t, back.Execution)
	assert.Equal(t, "FILLED", back.Execution.Status)
}

func TestTypeConstants(t *testing.T) {
	// 确保常量值与 docs/系统总体拓扑结构.md 5.2 表格一致
	assert.Equal(t, Type("auth"), TypeAuth)
	assert.Equal(t, Type("auth_result"), TypeAuthResult)
	assert.Equal(t, Type("heartbeat"), TypeHeartbeat)
	assert.Equal(t, Type("heartbeat_ack"), TypeHeartbeatAck)
	assert.Equal(t, Type("command"), TypeCommand)
	assert.Equal(t, Type("command_ack"), TypeCommandAck)
	assert.Equal(t, Type("delta_report"), TypeDeltaReport)
	assert.Equal(t, Type("report_ack"), TypeReportAck)
}
