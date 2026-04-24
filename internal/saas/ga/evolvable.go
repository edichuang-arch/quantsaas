// Package ga 是 GA 进化引擎 + 策略侧 Evolvable 实现的集合包。
//
// 包位置约束（进化文档 8.4）：
//
//   [Strategy]Evolvable 必须放在本包，而不是 strategy 包内。
//   原因：backtest 包已经导入 strategy 包（取其 Step()）；
//   若把 Evolvable 放回 strategy 包，strategy 就需要导入 backtest + ga，
//   形成 strategy ↔ (backtest, ga) 的循环依赖。
//   把 Evolvable 放在 ga 包内则单向依赖：ga → strategy → quant。
package ga

import (
	"math/rand"

	"github.com/edi/quantsaas/internal/quant"
)

// Gene 是不透明载体（engine.go 不读取其内部字段，仅通过 EvolvableStrategy 操作）。
type Gene = any

// DCABaseline Epoch 启动时对每个坩埚窗口预计算的 Ghost DCA 基线。
// 代内所有个体复用同一份基线，不重复跑被动 DCA。
type DCABaseline struct {
	FinalEquity   float64
	TotalInjected float64
	MaxDrawdown   float64
	ROI           float64
}

// EvaluablePlan Epoch 启动时构建，整个世代内只读。
// 传给每个 Evaluate 调用；引擎保证它不被修改。
type EvaluablePlan struct {
	Pair          string
	TemplateName  string
	Spawn         *quant.SpawnPoint
	LotStep       float64
	LotMin        float64
	Windows       []quant.CrucibleWindow
	DCABaselines  []DCABaseline // 与 Windows 一一对应
	InitialUSDT   float64       // 回测起始资本
	MonthlyInject float64       // 回测月度注资
}

// EvolvableStrategy 是引擎与具体策略之间的唯一接口（进化文档 8.1）。
// 引擎对 Chromosome 的字段完全不可见：新增策略只需实现此接口，无需改 engine.go。
type EvolvableStrategy interface {
	// StrategyID 策略唯一标识（如 "sigmoid-btc"）
	StrategyID() string

	// Sample 从合法基因空间随机采样一个基因。
	Sample(rng *rand.Rand) Gene

	// Mutate 对基因施加加性高斯变异。prob/scale 由引擎在代内动态调整。
	Mutate(c Gene, prob, scale float64, rng *rand.Rand) Gene

	// Crossover 对两个父代执行均匀交叉，产出子代。
	Crossover(p1, p2 Gene, rng *rand.Rand) Gene

	// Fingerprint 返回基因的唯一哈希（FNV-1a-64，精度 1e-6）。
	// 用于 Epoch 内指纹缓存去重。
	Fingerprint(c Gene) uint64

	// Evaluate 在给定坩埚计划上评估基因。
	// 按窗口从短到长级联评估，fatal 时立即返回（短路）。
	// 返回：总分（ScoreTotal）、各窗口明细、是否 fatal、最大回撤（供基因记录展示）。
	Evaluate(plan *EvaluablePlan, c Gene) EvalResult

	// DecodeElite 从 DB ParamPack JSON 解码精英基因。空/无效时返回默认种子。
	DecodeElite(raw []byte) Gene

	// EncodeResult 将冠军基因 + 出生点序列化为 ParamPack JSON blob。
	EncodeResult(c Gene, spawn *quant.SpawnPoint) ([]byte, error)
}

// EvalResult 一次基因评估的完整结果。
type EvalResult struct {
	ScoreTotal  float64
	Results     []quant.CrucibleResult
	Fatal       bool
	MaxDrawdown float64 // 所有窗口中的最大值
}

// Population 种群（Gene 数组）。封装以便后续加分代、适应度缓存等字段。
type Population struct {
	Genes    []Gene
	Fitness  []float64
	FatalMap []bool
}

// NewPopulation 构造一个指定大小的空种群。
func NewPopulation(size int) *Population {
	return &Population{
		Genes:    make([]Gene, size),
		Fitness:  make([]float64, size),
		FatalMap: make([]bool, size),
	}
}

// FatalFitnessScore 灾难性极小值。超过 MaxDD 88% 时的 SliceScore 值（进化文档 3.1）。
// 各窗口 fatal 后加权求和仍极低，锦标赛几乎不会选中。
const FatalFitnessScore = -99999.0

// FatalDrawdownThreshold 硬否决回撤阈值。
const FatalDrawdownThreshold = 0.88
