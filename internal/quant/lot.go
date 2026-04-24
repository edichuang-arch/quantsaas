package quant

import (
	"sort"
	"time"
)

// Lot 仓位 lot 的值对象（与 store.SpotLot 对应，但不带 DB 字段）。
// 策略与回测用这个类型做仓位推演；真正落库由 SaaS 侧适配器处理。
type Lot struct {
	LotType      LotType
	Amount       float64
	CostPrice    float64
	IsColdSealed bool
	CreatedAt    time.Time
}

// DeadStackAmount 活跃底仓总量（不含 ColdSealed）。
func DeadStackAmount(lots []Lot) float64 {
	sum := 0.0
	for _, l := range lots {
		if l.LotType == LotDeadStack && !l.IsColdSealed {
			sum += l.Amount
		}
	}
	return sum
}

// FloatStackAmount 浮动仓总量。
func FloatStackAmount(lots []Lot) float64 {
	sum := 0.0
	for _, l := range lots {
		if l.LotType == LotFloating {
			sum += l.Amount
		}
	}
	return sum
}

// ColdSealedAmount 冷封存总量（IsColdSealed=true 的 lots 累计，无论 LotType）。
func ColdSealedAmount(lots []Lot) float64 {
	sum := 0.0
	for _, l := range lots {
		if l.IsColdSealed {
			sum += l.Amount
		}
	}
	return sum
}

// SoftReleaseConfig 软释放参数。
type SoftReleaseConfig struct {
	MinAgeMonths int     // 最小老化月数（只有老过此阈值的 lot 才参与软释放）
	MaxRatio     float64 // 本次最多释放 DeadStack 的比例，范围 [0, 1]
	NowMs        int64   // 当前时间戳，用于计算 lot 老化
}

// SoftReleaseAmount 计算软释放可转出数量。
// 规则：
//   - 只考虑 LotDeadStack 且 !IsColdSealed
//   - lot 必须已老化超过 MinAgeMonths
//   - 总释放量 ≤ DeadStack × MaxRatio
//   - 按 CreatedAt 升序（老的先释放）累加，直到达到上限
//
// 返回可释放的总数量；具体哪些 lot 转换由上层 SaaS 账本处理（按相同顺序扣减）。
func SoftReleaseAmount(lots []Lot, cfg SoftReleaseConfig) float64 {
	if cfg.MaxRatio <= 0 {
		return 0
	}
	cutoff := cfg.NowMs - int64(cfg.MinAgeMonths)*30*24*60*60*1000

	eligible := make([]Lot, 0, len(lots))
	for _, l := range lots {
		if l.LotType != LotDeadStack || l.IsColdSealed {
			continue
		}
		if l.CreatedAt.UnixMilli() <= cutoff {
			eligible = append(eligible, l)
		}
	}
	if len(eligible) == 0 {
		return 0
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		return eligible[i].CreatedAt.Before(eligible[j].CreatedAt)
	})

	cap := DeadStackAmount(lots) * cfg.MaxRatio
	var released float64
	for _, l := range eligible {
		remain := cap - released
		if remain <= 0 {
			break
		}
		if l.Amount <= remain {
			released += l.Amount
		} else {
			released += remain
		}
	}
	return released
}

// HardReleaseAmount 计算硬释放需求（当微观卖出意图超过浮动仓余量时）。
// 参数：需要补足的缺口（正数）、当前 lots。
// 规则：从 DeadStack（非 ColdSealed，任何年龄）按 FIFO 累加，最多补到 gap。
// 返回实际可补足的数量（受 DeadStack 总量限制）。
// 铁律：ColdSealed 永不释放。
func HardReleaseAmount(lots []Lot, gap float64) float64 {
	if gap <= 0 {
		return 0
	}
	available := DeadStackAmount(lots)
	if available <= 0 {
		return 0
	}
	if gap > available {
		return available
	}
	return gap
}
