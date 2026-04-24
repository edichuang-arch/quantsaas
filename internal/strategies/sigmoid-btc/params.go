package sigmoidbtc

import "github.com/edi/quantsaas/internal/quant"

// Params 是本策略一次 Step() 决策所需的全部参数。
// 从 DB 的 ParamPack JSON blob 解码得到。
type Params struct {
	Chromosome quant.Chromosome
	Spawn      quant.SpawnPoint
}

// LoadParams 从 ParamPack JSON blob 反序列化。
// 空 blob / 解析失败时自动回退到默认种子（保证实盘永远有合法参数可用）。
func LoadParams(raw []byte) Params {
	c, sp := quant.DecodeParamPack(raw)
	return Params{Chromosome: c, Spawn: sp}
}

// DefaultParams 返回产品默认冠军，用于 GA 冷启动或单测初始化。
func DefaultParams() Params {
	return Params{
		Chromosome: quant.DefaultSeedChromosome,
		Spawn:      quant.DefaultSpawnPoint,
	}
}
