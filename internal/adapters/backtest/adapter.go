// Package backtest 把"策略 Step() 纯函数"翻译成一个可以遍历历史 K 线的模拟器。
//
// 铁律 #2：回测和实盘调用同一个 Step() 实现。
// 本包负责：
//   1. 按时间轴遍历 bars，每根 bar 驱动一次 Step()
//   2. 把策略产出的 TradeIntent 模拟成交（含手续费 + 最小下单量 + lot step 截断）
//   3. 维护 Portfolio 三态账本（DeadStack / FloatStack / ColdSealed）
//   4. 执行底仓释放（DEAD_STACK → FLOATING 语义转换，不经交易所）
//   5. 追踪 NAV 曲线、成交对数收益率、注资事件
//
// 本包不调用网络/数据库；纯内存模拟。
package backtest

import (
	"github.com/edi/quantsaas/internal/quant"
)

// Execution 单次策略的 Step() 调用后，所有意图被撮合后的执行结果。
type Execution struct {
	FilledQty   float64
	FilledPrice float64
	FilledUSDT  float64
	Fee         float64
	Action      quant.TradeAction
	Engine      quant.EngineKind
	LotType     quant.LotType
}

// Result 单次回测的聚合结果。
type Result struct {
	FinalEquity  float64
	MaxDrawdown  float64
	NumTrades    int
	TotalFeeUSDT float64
	NAVSeries    []float64
	TradeLogRets []float64 // 每笔成交的对数收益率（用于蒙特卡洛分析）
	CashFlows    []float64 // 注资金额序列（Modified Dietz 用）
	CashFlowDays []int     // 对应注资发生日（从 0 起）
	TotalDays    int
}

// StepFunc 策略入口的类型。所有现货策略的 Step() 必须与此签名一致。
type StepFunc func(quant.StrategyInput, any) quant.StrategyOutput

// Config 回测配置。
type Config struct {
	// InitialUSDT 模拟账户起始 USDT 余额。
	InitialUSDT float64
	// MonthlyInjectUSDT 每自然月月初注资金额（模拟真实实例的月度补仓计划）。
	MonthlyInjectUSDT float64
	// LotStep / LotMin 交易所下单精度（Binance 现货 BTC：0.00001 / 0.00001）。
	LotStep float64
	LotMin  float64
	// TakerFeeBps 单边手续费（万分之一）。10 = 0.10%。
	TakerFeeBps int
	// ParamsBlob 策略的 ParamPack JSON blob，传给 Step() 作为 params 参数的序列化形式。
	// 适配器不做类型解析；策略自己 LoadParams。
	ParamsBlob []byte
	// LoadParams 策略层提供的参数加载函数（避免适配器反向依赖策略包）。
	// 传入 ParamsBlob，返回 Step() 需要的 params（any 类型，实现自定义）。
	LoadParams func([]byte) any
	// Step 策略入口（StepFunc 签名）。
	Step StepFunc
	// Symbol 用于 StrategyInput，通常是 "BTCUSDT"。
	Symbol string
	// OnBar 可选的诊断 callback，每根 bar 在 Step + 撮合 + NAV 更新后调用一次。
	// nil 时完全不影响性能/行为。用于 debug 类 probe（找 MaxDD 炸点等）。
	OnBar func(BarDiag)
}

// BarDiag backtest 单根 bar 的诊断快照。
type BarDiag struct {
	I            int     // bar 索引
	OpenTime     int64   // bar 开始时间（毫秒）
	Price        float64 // 当根 close
	USDTBalance  float64
	DeadStack    float64
	FloatStack   float64
	ColdSealed   float64
	Equity       float64
	IsEvalRange  bool   // false 表示在 warmup 区间
	NumIntents   int    // 本根 bar 策略产出的 intent 数
	NumExecuted  int    // 本根 bar 实际成交的 intent 数（被资金不足/最小量等拒绝的不算）
	BuyExecUSDT  float64 // 本根 bar 实际买入 USDT 总额
	SellExecUSDT float64 // 本根 bar 实际卖出 USDT 总额
	DecisionReason string
}

// Run 按时间升序遍历 bars，驱动 Step()，返回 Result。
// EvalStartMs 之前的 bar 只用作 warmup：照样驱动 Step 但 NAV 不记录（保证 warmup 指标充分预热）。
func Run(bars []quant.Bar, cfg Config, evalStartMs int64) Result {
	res := Result{}
	if len(bars) == 0 || cfg.Step == nil {
		return res
	}

	// 初始化账本
	state := newLedger(cfg.InitialUSDT)
	rt := quant.RuntimeState{Extras: map[string]float64{}}
	params := cfg.LoadParams(cfg.ParamsBlob)

	// 用于 Modified Dietz 的起点 ms
	evalStartMs = maxI64(evalStartMs, bars[0].OpenTime)
	startIdx := indexOfEvalStart(bars, evalStartMs)
	if startIdx >= len(bars) {
		// 全量 warmup（evalStart 超出数据尾部），没有 eval 区间
		res.TotalDays = 0
		return res
	}
	totalDays := int((bars[len(bars)-1].OpenTime-bars[startIdx].OpenTime)/msPerDay) + 1
	if totalDays <= 0 {
		totalDays = 1
	}
	res.TotalDays = totalDays

	// 月度注资追踪
	prevY, prevM, _ := civilFromMs(bars[startIdx].OpenTime)

	// 闭合价序列：Step() 只接受 []float64
	allCloses := quant.ExtractCloses(bars)
	allTS := quant.ExtractTimestamps(bars)

	var peak float64

	for i, b := range bars {
		// 月度注资（只在 evalStart 之后触发，warmup 阶段不注资）
		if i >= startIdx && cfg.MonthlyInjectUSDT > 0 {
			y, m, _ := civilFromMs(b.OpenTime)
			if y != prevY || m != prevM {
				state.USDTBalance += cfg.MonthlyInjectUSDT
				res.CashFlows = append(res.CashFlows, cfg.MonthlyInjectUSDT)
				res.CashFlowDays = append(res.CashFlowDays, int((b.OpenTime-bars[startIdx].OpenTime)/msPerDay))
				prevY, prevM = y, m
			}
		}

		// 构建 Step 输入
		closesSlice := allCloses[:i+1]
		tsSlice := allTS[:i+1]
		snapshot := quant.PortfolioSnapshot{
			USDTBalance:     state.USDTBalance,
			DeadStackAsset:  state.DeadStack,
			FloatStackAsset: state.FloatStack,
			ColdSealedAsset: state.ColdSealed,
			CurrentPrice:    b.Close,
		}
		in := quant.StrategyInput{
			NowMs:             b.OpenTime,
			Closes:            closesSlice,
			Timestamps:        tsSlice,
			Portfolio:         snapshot,
			Symbol:            cfg.Symbol,
			MonthlyInjectUSDT: cfg.MonthlyInjectUSDT,
			LotStep:           cfg.LotStep,
			LotMin:            cfg.LotMin,
			PrevRuntime:       rt,
		}
		out := cfg.Step(in, params)
		rt = out.NewRuntime

		// 撮合意图（只在 evalStart 之后统计成交，warmup 阶段不产生交易记录）
		var diagBuyUSDT, diagSellUSDT float64
		var diagExecuted int
		if i >= startIdx {
			for _, intent := range out.Intents {
				ex, ok := matchIntent(state, intent, b.Close, cfg)
				if !ok {
					continue
				}
				applyExecution(state, ex)
				res.NumTrades++
				res.TotalFeeUSDT += ex.Fee
				diagExecuted++
				if ex.Action == quant.ActionBuy {
					diagBuyUSDT += ex.FilledUSDT
				} else {
					diagSellUSDT += ex.FilledUSDT
				}
				_ = ex
			}
			for _, rel := range out.Releases {
				// 底仓释放：DeadStack → FloatStack，纯账本更新，不经交易所、不计手续费
				amount := rel.Amount
				if amount > state.DeadStack {
					amount = state.DeadStack
				}
				state.DeadStack -= amount
				state.FloatStack += amount
			}
		}

		// NAV
		equity := state.equity(b.Close)
		if i >= startIdx {
			res.NAVSeries = append(res.NAVSeries, equity)
			if equity > peak {
				peak = equity
			}
			if peak > 0 {
				dd := (peak - equity) / peak
				if dd > res.MaxDrawdown {
					res.MaxDrawdown = dd
				}
			}
		}

		// 诊断 callback（仅 OnBar 非 nil 时有开销）
		if cfg.OnBar != nil {
			cfg.OnBar(BarDiag{
				I:              i,
				OpenTime:       b.OpenTime,
				Price:          b.Close,
				USDTBalance:    state.USDTBalance,
				DeadStack:      state.DeadStack,
				FloatStack:     state.FloatStack,
				ColdSealed:     state.ColdSealed,
				Equity:         equity,
				IsEvalRange:    i >= startIdx,
				NumIntents:     len(out.Intents),
				NumExecuted:    diagExecuted,
				BuyExecUSDT:    diagBuyUSDT,
				SellExecUSDT:   diagSellUSDT,
				DecisionReason: out.DecisionReason,
			})
		}
	}

	res.FinalEquity = state.equity(bars[len(bars)-1].Close)
	return res
}

// ledger 回测用的账本结构（内部）。
type ledger struct {
	USDTBalance float64
	DeadStack   float64
	FloatStack  float64
	ColdSealed  float64
}

func newLedger(initial float64) *ledger {
	return &ledger{USDTBalance: initial}
}

func (l *ledger) equity(price float64) float64 {
	return l.USDTBalance + (l.DeadStack+l.FloatStack+l.ColdSealed)*price
}

// matchIntent 模拟撮合（市价单、立即成交、一次性全量）。
// 返回 ok=false 表示意图被拒绝（资金不足、数量小于 LotMin 等）。
func matchIntent(l *ledger, intent quant.TradeIntent, price float64, cfg Config) (Execution, bool) {
	feeRate := float64(cfg.TakerFeeBps) / 10000.0
	if intent.Action == quant.ActionBuy {
		amt := intent.AmountUSDT
		if amt <= 0 || amt > l.USDTBalance {
			return Execution{}, false
		}
		// 根据 LotStep 截断实际成交数量
		qty := amt / price
		qty = truncateToStep(qty, cfg.LotStep)
		if qty < cfg.LotMin || qty <= 0 {
			return Execution{}, false
		}
		actualCost := qty * price
		fee := actualCost * feeRate
		if actualCost+fee > l.USDTBalance {
			return Execution{}, false
		}
		return Execution{
			FilledQty:   qty,
			FilledPrice: price,
			FilledUSDT:  actualCost,
			Fee:         fee,
			Action:      intent.Action,
			Engine:      intent.Engine,
			LotType:     intent.LotType,
		}, true
	}
	// SELL
	qty := intent.QtyAsset
	if qty <= 0 || qty > l.FloatStack {
		return Execution{}, false
	}
	qty = truncateToStep(qty, cfg.LotStep)
	if qty < cfg.LotMin || qty <= 0 {
		return Execution{}, false
	}
	gross := qty * price
	fee := gross * feeRate
	return Execution{
		FilledQty:   qty,
		FilledPrice: price,
		FilledUSDT:  gross,
		Fee:         fee,
		Action:      intent.Action,
		Engine:      intent.Engine,
		LotType:     intent.LotType,
	}, true
}

// applyExecution 用成交结果更新 ledger。
func applyExecution(l *ledger, ex Execution) {
	if ex.Action == quant.ActionBuy {
		l.USDTBalance -= (ex.FilledUSDT + ex.Fee)
		switch ex.LotType {
		case quant.LotDeadStack:
			l.DeadStack += ex.FilledQty
		case quant.LotFloating:
			l.FloatStack += ex.FilledQty
		}
	} else {
		// SELL：先扣资产，再加 USDT（扣掉手续费）
		switch ex.LotType {
		case quant.LotFloating:
			l.FloatStack -= ex.FilledQty
		case quant.LotDeadStack:
			l.DeadStack -= ex.FilledQty
		}
		l.USDTBalance += (ex.FilledUSDT - ex.Fee)
	}
}

// truncateToStep 按 step 向下截断（例如 step=0.001 时 0.12345 → 0.123）。
// step <= 0 时不截断。
func truncateToStep(x, step float64) float64 {
	if step <= 0 {
		return x
	}
	steps := int64(x / step)
	return float64(steps) * step
}

// indexOfEvalStart 返回第一个 OpenTime >= evalStartMs 的 bar 索引。
// 若没有任何 bar 满足（evalStart 超出全部数据）返回 len(bars)，
// 这样上层的 "i >= startIdx" 判断永远为 false，完整 warmup 区间被跳过。
func indexOfEvalStart(bars []quant.Bar, evalStartMs int64) int {
	for i, b := range bars {
		if b.OpenTime >= evalStartMs {
			return i
		}
	}
	return len(bars)
}

func maxI64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// --- 本包内的日历工具（避免依赖 quant 包内部的私有函数） ---

const msPerDay int64 = 24 * 60 * 60 * 1000

func civilFromMs(ms int64) (year, month, day int) {
	days := ms / msPerDay
	days += 719468
	era := days / 146097
	if days < 0 && days%146097 != 0 {
		era--
	}
	doe := days - era*146097
	yoe := (doe - doe/1460 + doe/36524 - doe/146096) / 365
	y := yoe + era*400
	doy := doe - (365*yoe + yoe/4 - yoe/100)
	mp := (5*doy + 2) / 153
	d := doy - (153*mp+2)/5 + 1
	m := mp + 3
	if mp >= 10 {
		m = mp - 9
		y++
	}
	return int(y), int(m), int(d)
}
