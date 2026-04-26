package quant

import (
	"encoding/json"
	"math"
)

// Chromosome 是 sigmoid-btc 策略的可进化参数集合。
// GA 对 16 个浮点字段做均匀交叉与加性高斯变异；字段边界由 HardBounds 定义。
// 参数分成三组：
//   - 微观 Sigmoid（β / γ / SigmaFloor）
//   - 微观楔形过滤（MinTradeDelta / WedgeVolThreshold）  [预留：当前以常量实现]
//   - 宏观 DCA（BaseDays / Multiplier / BetaThreshold / PriceDiscountBoost / DeadlineForcePct）
//   - 市场状态乘数（BullTimeDilation / BearTimeDilation / QuietBetaMultiplier）
type Chromosome struct {
	// 微观 Sigmoid
	Beta       float64 `json:"beta"`
	Gamma      float64 `json:"gamma"`
	SigmaFloor float64 `json:"sigma_floor"`

	// 宏观 DCA
	BaseDays           int     `json:"base_days"`
	Multiplier         float64 `json:"multiplier"`
	BetaThreshold     float64 `json:"beta_threshold"`
	PriceDiscountBoost float64 `json:"price_discount_boost"`
	DeadlineForcePct   float64 `json:"deadline_force_pct"`

	// 底仓释放规则
	MinAgeMonths        int     `json:"min_age_months"`
	SoftReleaseMaxRatio float64 `json:"soft_release_max_ratio"`

	// 市场状态乘数（可进化，决定不同市场环境下的加速/减速）
	BullTimeDilation      float64 `json:"bull_time_dilation"`
	BearTimeDilation      float64 `json:"bear_time_dilation"`
	BullBetaMultiplier    float64 `json:"bull_beta_multiplier"`
	BearBetaMultiplier    float64 `json:"bear_beta_multiplier"`

	// 微观资金保留比例（Plan 参数参考表：默认 0.25）
	MicroReservePct float64 `json:"micro_reserve_pct"`
}

// Bound 单个字段的合法数值范围（含两端）+ 初始采样的较窄子区间。
//
// 字段语义：
//   - Min/Max：HardBounds 硬边界。Mutate 在此范围内夹紧；Clamp 强制约束。
//   - InitMin/InitMax：初始随机采样的“合理区”。GA gen-0 在此区间内 uniform 采样，
//     避免随机撒点落在策略地狱区导致全员 fatal（MaxDD ≥ 88%）。
//     必须满足 Min ≤ InitMin ≤ InitMax ≤ Max。
//   - Step：变异步长（N(0,1) × Step × scale）。
//
// 设计取舍：mutate 仍用 [Min, Max] —— 让 GA 后期能突破 InitBounds 探索极端值，
// 只是 gen-0 不直接乱撒。
type Bound struct {
	Min, Max         float64
	InitMin, InitMax float64
	Step             float64
}

// HardBounds 每个基因字段的硬边界 + 初始化采样窄区间 + 变异步长。
//
// InitBounds 是基于 sigmoid 数学与 BTC 5m K 线量级的经验值：
//   - Beta/Gamma：避免 sigmoid 饱和（>2 时几乎全 0/1）
//   - SigmaFloor：BTC 5m σ 量级 50–200
//   - 时间膨胀/Beta 乘数：围绕 1× 小幅扰动
var HardBounds = struct {
	Beta                Bound
	Gamma               Bound
	SigmaFloor          Bound
	BaseDays            Bound
	Multiplier          Bound
	BetaThreshold       Bound
	PriceDiscountBoost  Bound
	DeadlineForcePct    Bound
	MinAgeMonths        Bound
	SoftReleaseMaxRatio Bound
	BullTimeDilation    Bound
	BearTimeDilation    Bound
	BullBetaMultiplier  Bound
	BearBetaMultiplier  Bound
	MicroReservePct     Bound
}{
	Beta:                Bound{Min: 0.1, Max: 5.0, InitMin: 0.5, InitMax: 2.0, Step: 0.3},
	Gamma:               Bound{Min: 0.0, Max: 3.0, InitMin: 0.0, InitMax: 1.0, Step: 0.2},
	SigmaFloor:          Bound{Min: 0.0, Max: 500.0, InitMin: 50.0, InitMax: 200.0, Step: 20.0},
	BaseDays:            Bound{Min: 1, Max: 30, InitMin: 7, InitMax: 21, Step: 2},
	Multiplier:          Bound{Min: 0.2, Max: 3.0, InitMin: 0.5, InitMax: 1.5, Step: 0.2},
	BetaThreshold:       Bound{Min: 0.0, Max: 0.3, InitMin: 0.05, InitMax: 0.15, Step: 0.02},
	PriceDiscountBoost:  Bound{Min: 0.0, Max: 5.0, InitMin: 0.5, InitMax: 2.0, Step: 0.5},
	DeadlineForcePct:    Bound{Min: 0.0, Max: 1.0, InitMin: 0.3, InitMax: 0.7, Step: 0.1},
	MinAgeMonths:        Bound{Min: 1, Max: 36, InitMin: 6, InitMax: 18, Step: 2},
	SoftReleaseMaxRatio: Bound{Min: 0.0, Max: 0.5, InitMin: 0.10, InitMax: 0.30, Step: 0.05},
	BullTimeDilation:    Bound{Min: 0.5, Max: 3.0, InitMin: 0.8, InitMax: 1.8, Step: 0.2},
	BearTimeDilation:    Bound{Min: 0.3, Max: 2.0, InitMin: 0.7, InitMax: 1.3, Step: 0.2},
	BullBetaMultiplier:  Bound{Min: 1.0, Max: 3.0, InitMin: 1.0, InitMax: 1.8, Step: 0.2},
	BearBetaMultiplier:  Bound{Min: 1.0, Max: 3.0, InitMin: 1.0, InitMax: 1.8, Step: 0.2},
	MicroReservePct:     Bound{Min: 0.0, Max: 0.8, InitMin: 0.10, InitMax: 0.40, Step: 0.05},
}

// DefaultSeedChromosome 产品默认冠军种子，作为 GA 冷启动初始个体或 JSON 解码失败的回退。
// 数值经验值，非精调结果，仅保证合法并有基本动能。
var DefaultSeedChromosome = Chromosome{
	Beta:                 1.5,
	Gamma:                1.0,
	SigmaFloor:           50.0,
	BaseDays:             7,
	Multiplier:           1.0,
	BetaThreshold:        0.05,
	PriceDiscountBoost:   1.5,
	DeadlineForcePct:     0.5,
	MinAgeMonths:         6,
	SoftReleaseMaxRatio:  0.10,
	BullTimeDilation:     1.5,
	BearTimeDilation:     0.75,
	BullBetaMultiplier:   1.3,
	BearBetaMultiplier:   1.3,
	MicroReservePct:      0.25,
}

// ClampChromosome 将所有字段夹紧到 HardBounds 内，并修复结构约束。
// 变异或交叉之后必须调用。
func ClampChromosome(c Chromosome) Chromosome {
	c.Beta = ClipFloat64(c.Beta, HardBounds.Beta.Min, HardBounds.Beta.Max)
	c.Gamma = ClipFloat64(c.Gamma, HardBounds.Gamma.Min, HardBounds.Gamma.Max)
	c.SigmaFloor = ClipFloat64(c.SigmaFloor, HardBounds.SigmaFloor.Min, HardBounds.SigmaFloor.Max)
	c.BaseDays = clampInt(c.BaseDays, int(HardBounds.BaseDays.Min), int(HardBounds.BaseDays.Max))
	c.Multiplier = ClipFloat64(c.Multiplier, HardBounds.Multiplier.Min, HardBounds.Multiplier.Max)
	c.BetaThreshold = ClipFloat64(c.BetaThreshold, HardBounds.BetaThreshold.Min, HardBounds.BetaThreshold.Max)
	c.PriceDiscountBoost = ClipFloat64(c.PriceDiscountBoost, HardBounds.PriceDiscountBoost.Min, HardBounds.PriceDiscountBoost.Max)
	c.DeadlineForcePct = ClipFloat64(c.DeadlineForcePct, HardBounds.DeadlineForcePct.Min, HardBounds.DeadlineForcePct.Max)
	c.MinAgeMonths = clampInt(c.MinAgeMonths, int(HardBounds.MinAgeMonths.Min), int(HardBounds.MinAgeMonths.Max))
	c.SoftReleaseMaxRatio = ClipFloat64(c.SoftReleaseMaxRatio, HardBounds.SoftReleaseMaxRatio.Min, HardBounds.SoftReleaseMaxRatio.Max)
	c.BullTimeDilation = ClipFloat64(c.BullTimeDilation, HardBounds.BullTimeDilation.Min, HardBounds.BullTimeDilation.Max)
	c.BearTimeDilation = ClipFloat64(c.BearTimeDilation, HardBounds.BearTimeDilation.Min, HardBounds.BearTimeDilation.Max)
	c.BullBetaMultiplier = ClipFloat64(c.BullBetaMultiplier, HardBounds.BullBetaMultiplier.Min, HardBounds.BullBetaMultiplier.Max)
	c.BearBetaMultiplier = ClipFloat64(c.BearBetaMultiplier, HardBounds.BearBetaMultiplier.Min, HardBounds.BearBetaMultiplier.Max)
	c.MicroReservePct = ClipFloat64(c.MicroReservePct, HardBounds.MicroReservePct.Min, HardBounds.MicroReservePct.Max)
	return c
}

func clampInt(x, lo, hi int) int {
	if x < lo {
		return lo
	}
	if x > hi {
		return hi
	}
	return x
}

// CapitalPolicy 资金政策（出生点的一部分）——不进入基因组。
type CapitalPolicy struct {
	DeadReserveRatio float64 `json:"dead_reserve_ratio"` // 死守的底仓下限占比
	GlobalStopLoss   float64 `json:"global_stop_loss"`   // 全局权益回撤熔断，0 = 关闭
}

// RiskBounds 风险边界（出生点的一部分）——不进入基因组。
type RiskBounds struct {
	MaxLeverage float64 `json:"max_leverage"` // 现货固定为 1
	TakerFeeBps int     `json:"taker_fee_bps"` // 单边手续费（万分之一）
}

// SpawnPoint 出生点：Epoch 级冻结，全种群共享，不参与代内交叉变异，不进入基因组指纹。
type SpawnPoint struct {
	Policy  CapitalPolicy `json:"policy"`
	Risk    RiskBounds    `json:"risk"`
}

// DefaultSpawnPoint 产品默认出生点。Binance 现货 taker 费率 ≈ 10 bps（0.1%）。
var DefaultSpawnPoint = SpawnPoint{
	Policy: CapitalPolicy{DeadReserveRatio: 0.30, GlobalStopLoss: 0},
	Risk:   RiskBounds{MaxLeverage: 1, TakerFeeBps: 10},
}

// ParamPack 数据库里 champion/challenger 记录的 ParamPack 字段内容。
type ParamPack struct {
	SpawnPoint      SpawnPoint `json:"spawn_point"`
	SigmoidBTCParams Chromosome `json:"sigmoid_btc_config"`
}

// EncodeParamPack 序列化为 JSON blob。
func EncodeParamPack(c Chromosome, sp SpawnPoint) ([]byte, error) {
	return json.Marshal(ParamPack{SpawnPoint: sp, SigmoidBTCParams: c})
}

// DecodeParamPack 解码 JSON blob；nil/空/解析失败时返回默认种子。
func DecodeParamPack(raw []byte) (Chromosome, SpawnPoint) {
	if len(raw) == 0 {
		return DefaultSeedChromosome, DefaultSpawnPoint
	}
	var p ParamPack
	if err := json.Unmarshal(raw, &p); err != nil {
		return DefaultSeedChromosome, DefaultSpawnPoint
	}
	// 确保任何缺失字段都有合理值
	c := ClampChromosome(p.SigmoidBTCParams)
	sp := p.SpawnPoint
	if sp.Risk.MaxLeverage == 0 {
		sp = DefaultSpawnPoint
	}
	return c, sp
}

// ValidateChromosome 额外结构检查（除 Clamp 之外的硬规则）。
func ValidateChromosome(c Chromosome) error {
	if math.IsNaN(c.Beta) || math.IsNaN(c.Gamma) || math.IsNaN(c.SigmaFloor) {
		return ErrChromosomeNaN
	}
	return nil
}

// ErrChromosomeNaN Chromosome 含 NaN 字段。
var ErrChromosomeNaN = stringError("chromosome contains NaN field")

type stringError string

func (e stringError) Error() string { return string(e) }
