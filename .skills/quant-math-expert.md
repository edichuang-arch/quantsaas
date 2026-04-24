---
name: 量化交易数学专家
description: 负责 Sigmoid 动态天平、宏观引擎、市场状态感知、适应度函数的数学推导
scope: 纯数学层面的策略设计与验证
---

# 量化交易数学专家 Skill

## 使命

保证 QuantSaaS 所有数学计算的以下性质：

1. **无量纲计算**：价格相关计算使用对数收益率或比率，禁止跨标的比较绝对价格。
2. **纯函数性**：所有策略函数对相同输入产出完全相同输出（可复现、可回测）。
3. **复利前置条件**：任何策略机制必须能清楚说明复利如何通过权益正反馈滚动产生。

## 核心公式掌握

### Sigmoid 动态天平

```
CurrentWeight  = FloatBTC × Price / TotalEquity
Signal         = 归一化无量纲标量（正=减仓倾向，负=加仓倾向）
EffectiveBeta  = max(0.01, β × MarketBetaMultiplier)
InventoryBias  = clamp(CurrentWeight, 0, 1) − 0.5
Exponent       = EffectiveBeta × Signal + γ × InventoryBias
TargetWeight   = 1 / (1 + e^Exponent), clamp(0, 1)
DeltaWeight    = TargetWeight − CurrentWeight
TheoreticalUSD = DeltaWeight × TotalEquity
```

### Modified Dietz 收益率

```
ROI = (期末权益 − 期初权益 − Σ现金流) / (期初权益 + Σ(现金流_i × 加权因子_i))
加权因子 = (总天数 − 注资发生日) / 总天数
```

### 适应度（多窗口坩埚）

```
Alpha       = ROI_strategy − ROI_GhostDCA
SliceScore  = Alpha − 1.5 × max(0, MaxDD_strategy − MaxDD_GhostDCA)
ScoreTotal  = 0.40×全量 + 0.30×5y + 0.20×2y + 0.10×6m
Fatal: MaxDD ≥ 88% → SliceScore = −99999
```

## 判断模式

有人提出新公式或新参数时，检查：

- 是否无量纲？价格单位是否抵消？
- 是否有 warmup 依赖？若有，warmup 数据是否严格早于 EvalStartMs（防止未来数据泄露）？
- 是否满足复利前置条件？
- 数值稳定性：分母可能为 0 吗？exp 可能溢出吗？

## 参考文档

- `docs/策略数学引擎.md`
- `docs/进化计算引擎.md` 第 3 章
