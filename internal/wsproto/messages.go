// Package wsproto 定义 SaaS 与 LocalAgent 之间的 WebSocket 消息协议。
//
// 所有消息共用 Envelope 外层，内部 Payload 字段按 Type 决定。
// Agent 与 SaaS 两端都只依赖本包的类型定义，不反向引用对方包。
package wsproto

// Type 枚举全部 8 种消息类型（docs/系统总体拓扑结构.md 5.2）。
type Type string

const (
	TypeAuth         Type = "auth"          // Agent → SaaS
	TypeAuthResult   Type = "auth_result"   // SaaS → Agent
	TypeHeartbeat    Type = "heartbeat"     // Agent → SaaS
	TypeHeartbeatAck Type = "heartbeat_ack" // SaaS → Agent
	TypeCommand      Type = "command"       // SaaS → Agent
	TypeCommandAck   Type = "command_ack"   // Agent → SaaS
	TypeDeltaReport  Type = "delta_report"  // Agent → SaaS
	TypeReportAck    Type = "report_ack"    // SaaS → Agent
)

// Envelope 是所有消息的外层结构。Type 决定 Payload 的具体字段。
// 发送端使用具体的 Payload 子类型填充；接收端先读 Type 再 unmarshal Payload。
type Envelope struct {
	Type    Type `json:"type"`
	Payload any  `json:"payload,omitempty"`
}

// AuthPayload Agent 发送的鉴权消息。
type AuthPayload struct {
	JWT        string `json:"jwt"`
	AgentVer   string `json:"agent_ver,omitempty"`
	Capability string `json:"capability,omitempty"` // e.g. "binance-spot"
}

// AuthResultPayload SaaS 返回的鉴权结果。
type AuthResultPayload struct {
	OK     bool   `json:"ok"`
	UserID uint   `json:"user_id,omitempty"`
	Error  string `json:"error,omitempty"`
}

// HeartbeatPayload 心跳（可携带 Agent 端最近心跳计数等诊断信息）。
type HeartbeatPayload struct {
	SentAtMs int64 `json:"sent_at_ms,omitempty"`
}

// HeartbeatAckPayload 心跳回执。
type HeartbeatAckPayload struct {
	ReceivedAtMs int64 `json:"received_at_ms,omitempty"`
}

// TradeCommand 是 SaaS 下发给 Agent 的单条交易指令。
// 与 docs/系统总体拓扑结构.md 5.3 字段语义一致。
type TradeCommand struct {
	ClientOrderID string `json:"client_order_id"`
	Action        string `json:"action"`                // BUY / SELL
	Engine        string `json:"engine"`                // MACRO / MICRO
	Symbol        string `json:"symbol"`                // e.g. "BTCUSDT"
	AmountUSDT    string `json:"amount_usdt,omitempty"` // BUY 时使用（字符串保留精度）
	QtyAsset      string `json:"qty_asset,omitempty"`   // SELL 时使用
	LotType       string `json:"lot_type"`              // DEAD_STACK / FLOATING
}

// CommandPayload 是 command 消息的 Payload（即 TradeCommand 本身）。
// 单独封装一层方便未来扩展 batch 指令、优先级等字段。
type CommandPayload struct {
	TradeCommand
}

// CommandAckPayload Agent 收到指令后立即回执（不等执行完成）。
type CommandAckPayload struct {
	ClientOrderID string `json:"client_order_id"`
}

// Balance 单资产的余额快照。
type Balance struct {
	Asset  string `json:"asset"`  // "BTC" / "USDT"
	Free   string `json:"free"`   // 可用余额（字符串保留精度）
	Locked string `json:"locked"` // 冻结金额
}

// ExecutionDetail 单笔成交的明细。
type ExecutionDetail struct {
	ClientOrderID string `json:"client_order_id"`
	Status        string `json:"status"` // "FILLED" / "FAILED" / "PARTIALLY_FILLED"
	FilledQty     string `json:"filled_qty"`
	FilledPrice   string `json:"filled_price"`
	FilledQuote   string `json:"filled_quote"` // 成交金额（USDT）
	Fee           string `json:"fee"`
	FeeAsset      string `json:"fee_asset"`
	ErrorMessage  string `json:"error_message,omitempty"`
}

// DeltaReport Agent 上报的增量状态。
//
//   - 执行后上报：ClientOrderID 非空 + Execution 有成交明细
//   - 重连初始快照：ClientOrderID 空 + Execution nil，仅 Balances 完整
type DeltaReport struct {
	ClientOrderID string           `json:"client_order_id,omitempty"`
	Balances      []Balance        `json:"balances"`
	Execution     *ExecutionDetail `json:"execution,omitempty"`
	Symbol        string           `json:"symbol,omitempty"`
	SentAtMs      int64            `json:"sent_at_ms,omitempty"`
}

// ReportAckPayload SaaS 确认已处理 DeltaReport。
type ReportAckPayload struct {
	ClientOrderID string `json:"client_order_id,omitempty"`
	OK            bool   `json:"ok"`
	Error         string `json:"error,omitempty"`
}
