package quant

// Bar 单根 K 线。现货策略内核禁止依赖此结构（ACL 外圈做 OHLCV→[]float64 降级）。
type Bar struct {
	OpenTime  int64   // 毫秒
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime int64
}

// PortfolioSnapshot 账户三态快照，Step() 决策输入的一部分。
// DeadStack 不含 ColdSealed；冷封存永不释放。
type PortfolioSnapshot struct {
	USDTBalance     float64
	DeadStackAsset  float64
	FloatStackAsset float64
	ColdSealedAsset float64
	// CurrentPrice 是最新成交价或收盘价，用于 equity 计算。
	CurrentPrice float64
}

// TotalEquity = 现金 + (DeadStack + FloatStack + ColdSealed) × Price
// 冷封存虽然不可卖，但仍计入总权益展示。
func (p PortfolioSnapshot) TotalEquity() float64 {
	asset := p.DeadStackAsset + p.FloatStackAsset + p.ColdSealedAsset
	return p.USDTBalance + asset*p.CurrentPrice
}

// SpendableAsset = DeadStack + FloatStack（微观引擎视角下可变现的资产）
// 不含 ColdSealed。
func (p PortfolioSnapshot) SpendableAsset() float64 {
	return p.DeadStackAsset + p.FloatStackAsset
}

// CurrentMicroWeight 微观引擎关心的权重定义：
//     (FloatStack × Price) / TotalEquity
// 即"浮动仓市值占总权益的比例"。
// TotalEquity = 0 时返回 0。
func (p PortfolioSnapshot) CurrentMicroWeight() float64 {
	eq := p.TotalEquity()
	if eq <= 0 {
		return 0
	}
	return (p.FloatStackAsset * p.CurrentPrice) / eq
}

// TradeAction 买卖方向枚举。大写字符串与 DB/API/WebSocket 保持一致。
type TradeAction string

const (
	ActionBuy  TradeAction = "BUY"
	ActionSell TradeAction = "SELL"
)

// EngineKind 产生该意图的引擎层。
type EngineKind string

const (
	EngineMacro EngineKind = "MACRO" // 宏观（DCA 买入 → DeadStack）
	EngineMicro EngineKind = "MICRO" // 微观（Sigmoid 买卖 → FloatStack）
)

// LotType 与 store.LotType 保持语义一致；quant 包独立定义以避免 strategies→store 反向依赖。
type LotType string

const (
	LotDeadStack  LotType = "DEAD_STACK"
	LotFloating   LotType = "FLOATING"
	LotColdSealed LotType = "COLD_SEALED"
)

// TradeIntent Step() 输出的交易意图。
// 适配器再翻译成 SpotExecution + TradeCommand（WebSocket 下发）。
type TradeIntent struct {
	Action     TradeAction
	Engine     EngineKind
	LotType    LotType // DEAD_STACK（宏观买入） / FLOATING（微观买卖）
	AmountUSDT float64 // BUY 时使用：花费 USDT 金额
	QtyAsset   float64 // SELL 时使用：卖出资产数量
	Note       string  // 可选：决策理由（调试用，不影响执行）
}

// ReleaseIntent 底仓释放意图（铁律 #9：仅更新 SaaS 侧账本，不下发 Agent）。
// 将指定数量从 DEAD_STACK（非 ColdSealed）转为 FLOATING。
type ReleaseIntent struct {
	Amount float64
	Reason string // "soft_age" / "hard_demand"
}

// StrategyInput 是 Step() 的唯一输入（快照语义，策略不应持有引用到外部可变状态）。
type StrategyInput struct {
	// 时间
	NowMs int64 // 当前 bar 的 OpenTime（毫秒），策略不得调用 time.Now()

	// 市场数据（ACL 外圈已从 []Bar 降级）
	Closes     []float64 // 收盘价序列，索引 0 为最早
	Timestamps []int64   // 与 Closes 一一对应的 bar OpenTime

	// 账户快照
	Portfolio PortfolioSnapshot

	// 外部运行时常量（由实例配置派生）
	Symbol             string
	MonthlyInjectUSDT  float64 // 实例的月度注资计划，宏观引擎用
	LotStep            float64 // Binance 下单精度（例 0.00001）
	LotMin             float64 // Binance 最小下单量

	// 上次持久化的运行时状态（策略可读可写；Step 返回 NewRuntimeState）。
	PrevRuntime RuntimeState
}

// RuntimeState 是策略自定义的跨 tick 持久化字段集。
// 定义在 quant 包是为了在 StrategyInput/Output 的签名里引用；
// 具体字段语义由各策略填充，此处仅提供通用容器。
type RuntimeState struct {
	// 上次成功推进的 bar 时间（毫秒），cron 幂等桶去重用。
	LastProcessedBarTime int64
	// 上次宏观决策时间，用于节奏控制。
	LastMacroDecisionMs int64
	// 当月已注资金额（自然月清零）。
	MonthlyInjectedUSDT float64
	// 当月起始时间戳，用于判断是否跨月。
	MonthAnchorMs int64
	// 策略自定义扩展字段：key → float64（避免频繁改 struct 导致 DB migration）。
	Extras map[string]float64
}

// StrategyOutput Step() 的唯一输出。
type StrategyOutput struct {
	Intents         []TradeIntent
	Releases        []ReleaseIntent
	NewRuntime      RuntimeState
	DecisionReason  string // 顶层决策摘要，写 AuditLog 用
	SkipReason      string // 非空表示本次 tick 主动跳过（数据不足等）
}
