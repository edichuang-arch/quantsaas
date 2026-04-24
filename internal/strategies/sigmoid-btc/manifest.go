// Package sigmoidbtc 实现 sigmoid-btc 策略的 Step() 主函数。
//
// 铁律清单（违反任何一条都是 bug）：
//
//   1. 本包的所有文件都**不可以**导入 net/http、database/sql、os（除 os.Getenv 外）、
//      io、time（除常量用途外），不得调用 time.Now。
//   2. 本包只能导入标准库与 internal/quant；不得反向导入 saas/* 或 agent/*。
//   3. Step() 内部不得出现 isBacktest 分支，回测与实盘共用本函数。
//   4. 只能消费 []float64 + []int64，不得依赖 quant.Bar。
//   5. RuntimeState 必须通过 StrategyInput.PrevRuntime 读入，在返回的 StrategyOutput 里写出。
package sigmoidbtc

import "github.com/edi/quantsaas/internal/quant"

// StrategyID 是进化引擎、基因记录、策略模板唯一标识符。
const StrategyID = "sigmoid-btc"

// StrategyName 面向开发与日志的可读名。用户界面展示的名称另由前端 i18n 映射。
const StrategyName = "Sigmoid Dynamic Balance BTC"

// StrategyVersion 每次发布一个冠军包时可以递增；主要用于审计与迁移。
const StrategyVersion = "0.1.0"

// IsSpot 本策略为现货（no futures, no leverage）。
const IsSpot = true

// SupportedExchange 对应 Binance Spot。
const SupportedExchange = "binance"

// SupportedSymbol 当前只做 BTC/USDT。
const SupportedSymbol = "BTCUSDT"

// Manifest 是策略模板的静态描述，供 SaaS 注册与前端展示使用。
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	IsSpot      bool   `json:"is_spot"`
	Exchange    string `json:"exchange"`
	Symbol      string `json:"symbol"`
	Description string `json:"description"`
	// ParamPackDefault 是 JSON 序列化后的默认 ParamPack，SaaS 初始化模板时写入。
	ParamPackDefault []byte `json:"-"`
}

// BuildManifest 生成当前模板的完整 Manifest，含默认 ParamPack。
func BuildManifest() (Manifest, error) {
	pack, err := quant.EncodeParamPack(quant.DefaultSeedChromosome, quant.DefaultSpawnPoint)
	if err != nil {
		return Manifest{}, err
	}
	return Manifest{
		ID:               StrategyID,
		Name:             StrategyName,
		Version:          StrategyVersion,
		IsSpot:           IsSpot,
		Exchange:         SupportedExchange,
		Symbol:           SupportedSymbol,
		Description:      "Sigmoid 动态天平微观引擎 + DCA 宏观引擎，现货 BTC/USDT",
		ParamPackDefault: pack,
	}, nil
}
