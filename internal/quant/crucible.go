package quant

// 多时段坩埚（Crucible）：把全量历史 K 线切成四个滚动窗口。
// 对每个窗口独立计算适应度 SliceScore，再按权重加权为 ScoreTotal。
//
// 铁律（进化文档 1.1）：
//   - 每个窗口包含 warmup 前缀 + eval 区间
//   - EvalStartMs 之后的数据是"可以被看到"的；之前的 warmup 只用于指标预热
//   - 禁止未来数据泄露：eval 区间任何计算都不得穿越 EvalStartMs
//
// 窗口权重按进化文档定义：
//
//   full (全量)  → 0.40
//   5y           → 0.30
//   2y           → 0.20
//   6m           → 0.10

// CrucibleWindow 单个评估窗口：含 warmup 前缀的 bars 切片 + 评分区间起点。
type CrucibleWindow struct {
	Label       string  // "6m" / "2y" / "5y" / "full"
	Weight      float64 // 适应度权重
	Bars        []Bar   // warmup + eval 段，按时间升序
	EvalStartMs int64   // 评分区间起点（毫秒）；Bars 中 OpenTime < 此值的都是 warmup
}

// CrucibleResult 单窗评估结果。
type CrucibleResult struct {
	Label       string
	Score       float64 // SliceScore = Alpha − 1.5 × max(0, MaxDD_strategy − MaxDD_GhostDCA)
	Alpha       float64 // ROI_strategy − ROI_GhostDCA
	ROI         float64
	MaxDrawdown float64
	Fatal       bool // MaxDD ≥ 88% 时为 true（进化文档 3.1）
}

const (
	defaultWarmupDays = 1200
	// 各窗口目标天数（进化文档 1.1 表格）
	eval6mDays  = 183
	eval2yDays  = 730
	eval5yDays  = 1825
	msPerDayQ   = int64(24 * 60 * 60 * 1000) // 等同 macro_engine 里 msPerDay；名字不同避免包内重定义
)

// WindowWeights 四个窗口的固定权重，总和 = 1.0（进化文档 1.1）。
var WindowWeights = map[string]float64{
	"full": 0.40,
	"5y":   0.30,
	"2y":   0.20,
	"6m":   0.10,
}

// BuildCrucibleWindows 构建四个坩埚窗口。
// 输入必须按 OpenTime 升序；warmupDays <= 0 时使用默认 1200。
// 窗口返回顺序按 bar 数量升序（6m → 2y → 5y → full），匹配级联短路顺序。
// 当全量 bars 不足以支撑某个窗口的 eval 区间时，该窗口被丢弃。
func BuildCrucibleWindows(bars []Bar, warmupDays int) []CrucibleWindow {
	if warmupDays <= 0 {
		warmupDays = defaultWarmupDays
	}
	if len(bars) == 0 {
		return nil
	}

	latestMs := bars[len(bars)-1].OpenTime
	warmupMs := int64(warmupDays) * msPerDayQ
	earliest := bars[0].OpenTime

	out := make([]CrucibleWindow, 0, 4)

	// 固定窗口：6m / 2y / 5y
	fixedWindows := []struct {
		label   string
		evalDays int
	}{
		{"6m", eval6mDays},
		{"2y", eval2yDays},
		{"5y", eval5yDays},
	}
	for _, fw := range fixedWindows {
		evalStart := latestMs - int64(fw.evalDays)*msPerDayQ
		barsStart := evalStart - warmupMs
		if barsStart < earliest {
			// 数据不够支撑完整 warmup，跳过此窗口
			continue
		}
		slice := sliceBarsFrom(bars, barsStart)
		if len(slice) == 0 {
			continue
		}
		out = append(out, CrucibleWindow{
			Label:       fw.label,
			Weight:      WindowWeights[fw.label],
			Bars:        slice,
			EvalStartMs: evalStart,
		})
	}

	// full 窗口：评分区间从最早可用 bar 到最新 bar；warmup 仅占最早的 warmupDays 天
	fullEvalStart := earliest + warmupMs
	if fullEvalStart > latestMs {
		// 数据总长度不足一个 warmup；直接使用全部数据作为 eval（不做 warmup）
		fullEvalStart = earliest
	}
	out = append(out, CrucibleWindow{
		Label:       "full",
		Weight:      WindowWeights["full"],
		Bars:        bars,
		EvalStartMs: fullEvalStart,
	})
	return out
}

// sliceBarsFrom 返回 bars 中 OpenTime >= startMs 的最小后缀片段。
// bars 假定已按 OpenTime 升序。
func sliceBarsFrom(bars []Bar, startMs int64) []Bar {
	// 二分查找 lower bound
	lo, hi := 0, len(bars)
	for lo < hi {
		mid := (lo + hi) / 2
		if bars[mid].OpenTime < startMs {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo >= len(bars) {
		return nil
	}
	return bars[lo:]
}

// EvalBars 返回指定窗口中 OpenTime >= EvalStartMs 的 bars（即评分区间）。
// 用于适应度打分时跟 Ghost DCA 对齐基准。
func (w CrucibleWindow) EvalBars() []Bar {
	return sliceBarsFrom(w.Bars, w.EvalStartMs)
}
