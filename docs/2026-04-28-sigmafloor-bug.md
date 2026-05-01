# SigmaFloor 絕對值 Bug 與硬風控修復

**日期**：2026-04-28（診斷）／ 2026-05-02（修復）
**影響範圍**：sigmoid-btc 策略所有 chromosome、所有運行模式
**嚴重度**：🔴 高 — 真實 BTC 365 天回測 MaxDD 達 98.29%（BTC 自身僅 52%，策略放大 1.88×）

---

## 一、發現過程

### 觸發條件

4/28 在前端 evolution 頁觸發 `test_mode=true` 的 GA epoch（Pop=10、Gen=3、warmup=60d、symbol=BTCUSDT）。

### 結果

| 指標 | 值 |
|---|---|
| Best Score | **−99999（fatal）** |
| 最佳個體 MaxDD | **98.29%** |
| 儲存的 challenger | 跟 DefaultSeedChromosome 完全相同（沒人比它好） |

**整代所有 10 個個體 + 3 代演化 = 30 次 evaluate 全部 fatal**（MaxDD ≥ 88% 觸發 cascade short-circuit）。

### 真實 BTC 365 天行情

| 指標 | 值 |
|---|---|
| 最低價 | $60,082 |
| 最高價 | $126,011 |
| 平均價 | $96,608 |
| 區間漲幅 | +110% |
| BTC 自身 peak-to-trough | **52.32%** |
| **策略回測跌幅** | **98.29%（放大 1.88×）** |

---

## 二、根因分析

### 死亡螺旋的數學機制

Sigmoid 動態天平在持續性下跌中會出現以下行為鏈：

```
1. BTC 跌 → Signal = (price − EMA) / σ 變負
2. Exponent = β × Signal + γ × InventoryBias 變負
3. TargetWeight = 1 / (1 + exp(Exponent)) → 接近 1（想滿倉）
4. DeltaWeight = TargetWeight − CurrentWeight 為正 → 觸發 BUY
5. USDT 燒乾，持倉接近 100%
6. BTC 繼續跌 → equity 同步跌
7. ⚠️ 跌到某個閾值，σ 計算失控 → 步驟 1-6 加劇
```

### 關鍵 Bug：σ 保護下限的單位錯誤

`Chromosome.SigmaFloor`（舊版）以 **絕對 USDT 值** 設定 σ 的下限：

```go
// micro_engine.go (舊版)
sigma := StdDev(closes, MicroSignalStdDevBars)
if in.SigmaFloor > 0 && sigma < in.SigmaFloor {
    sigma = in.SigmaFloor  // SigmaFloor 寫死 USDT 量級
}
```

**問題**：保護強度隨 BTC 價格漂移：

| BTC 價格 | SigmaFloor=50 USDT 占比 | 保護有效性 |
|---|---|---|
| $60,000 | 0.083% | ✅ 有效（合理保護平靜市場 σ→0） |
| $80,000 | 0.063% | ⚠️ 邊界 |
| $100,000 | 0.050% | ❌ 失效 |
| $126,000 | 0.040% | ❌ 嚴重失效 |

當 BTC 漲到 $100k+ 時，極平靜市場的真實 σ 可能 < $50（看 5m K 線），但 SigmaFloor=50 USDT 已經低於正常波動，不再起到「σ→0 防爆炸」的作用。

**結果**：σ 變得異常小（接近真實 stdDev），Signal = (price − EMA) / σ 數值爆炸到 ±10 以上 → β × Signal 進 sigmoid 完全飽和到 0 或 1 → 系統做出極端決策。

---

## 三、修復方案

### 路徑 A：把 SigmaFloor 改成百分比

**新欄位**：`Chromosome.SigmaFloorPct`（占當前價的百分比）

```go
// micro_engine.go (新版)
ema := EMA(closes, MicroSignalEMABars)
sigma := StdDev(closes, MicroSignalStdDevBars)
if in.SigmaFloorPct > 0 && in.CurrentPrice > 0 {
    floor := in.CurrentPrice * in.SigmaFloorPct
    if sigma < floor {
        sigma = floor
    }
}
```

**新 HardBounds**：

| 欄位 | Min | InitMin | InitMax | Max | DefaultSeed |
|---|---|---|---|---|---|
| SigmaFloorPct | 0 | 0.0005 (0.05%) | 0.0025 (0.25%) | 0.01 (1%) | **0.0008 (0.08%)** |

DefaultSeed 0.08% 等同於舊版 BTC $60k 時的 50 USDT 行為，向下相容。

### 路徑 B：硬風控守門（不進入基因組）

策略級硬常數，防止 GA 演化出繞過風控的解：

```go
// strategies/sigmoid-btc/step.go
const (
    HardCashFloorRatio  = 0.10  // USDT/Equity < 10% 時禁止 BUY
    HardPositionCeiling = 0.90  // 持倉/Equity > 90% 時禁止 BUY
)
```

**設計原則**：
1. 兩條守門只阻止 BUY，**SELL 永遠放行**（緊急情境下 SELL 是恢復流動性的手段）
2. 守門理由寫進 `StrategyOutput.DecisionReason`，運維一眼看到
3. Extras 記錄最後觸發時間戳，供 dashboard 顯示

**為什麼不進基因組**：GA 為了 ROI 會犧牲安全。若 cash floor 是基因，演化可能找出「在牛市末期把 floor 設 0% 全梭哈」的解，平均 ROI 高但 tail risk 災難。

---

## 四、影響範圍與向後相容

### 破壞性變更

| 變更 | 影響 |
|---|---|
| `Chromosome.SigmaFloor` → `SigmaFloorPct` | 既存 ParamPack JSON 失效 |
| `Bound{Min, Max}` 數值改 | 既存 challenger 解碼失敗 fallback 到 DefaultSeed |
| `MicroInput.SigmaFloor` → `SigmaFloorPct` | engine 計算行為改變 |

### 緩解

1. **DB 既存資料**：4/28 那筆 `gene_records.id=2` challenger 內容是 DefaultSeed copy，破壞它無實質損失
2. **未來 ParamPack 解碼**：`DecodeParamPack` 失敗會 fallback 到 `DefaultSeedChromosome`（已有保護）
3. **行為連續性**：DefaultSeed 的 `SigmaFloorPct=0.0008` 在 BTC $60k 時等同舊 50 USDT，不會突兀

### 不需要做的事

- ❌ 不需要寫 GORM migration（chromosome 是 JSON blob，欄位 rename 不影響表結構）
- ❌ 不需要清空 `gene_records`（自然 fallback）
- ❌ 不需要重啟 testnet 模擬資金（portfolio_states 不受影響）

---

## 五、驗證計劃

### 自動測試

- ✅ `TestSample_WithinInitBounds` — 200 samples 落在新 InitBounds 內
- ✅ `TestStep_HardCashFloorBlocksBuy` — 現金 4% 時 BUY 被阻
- ✅ `TestShouldBlockBuy_CashFloor` — 守門函式直接驗證
- ✅ `TestStep_BlockedTimestampsWritten` — 守門觸發寫入 Extras

### 手動驗證

修復後重啟 SaaS，觀察 1–2 週：
- [ ] testnet 在橫盤期不出現「USDT 燒乾」
- [ ] 當 BTC 大漲後又回落，倉位不會卡在 95%+
- [ ] 觸發 `/api/v1/evolution/tasks?test_mode=true`，best score 不再是 −99999

### 風險仍在的部分

修了 SigmaFloor 跟硬風控，但**不能保證 365 天回測 MaxDD < 88%**。需要實際跑一次 GA evolution 才能驗證：

```bash
# 修復後執行
curl -X POST http://localhost:8080/api/v1/evolution/tasks \
  -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"strategy_id":"sigmoid-btc","symbol":"BTCUSDT","initial_usdt":10000,
       "monthly_inject":300,"lot_step":0.00001,"lot_min":0.00001,
       "warmup_days":60,"test_mode":true}'
```

預期 best score > −99999（至少有個體不 fatal）。若仍全代 fatal，代表死亡螺旋有第三個未發現的根因。

---

## 六、學到的教訓

1. **單位錯誤 + 跨價格區間 = 隱形地雷**：絕對值參數在資產價格大幅漂移時會默默失效，回測通過不代表 production safe
2. **GA 不等於風控**：GA 為了 ROI 會犧牲尾部風險，硬風控必須脫離基因組
3. **合成資料 ≠ 真實資料**：GBM jump-diffusion 探測不出來的 bug，真實 BTC 365 天會炸
4. **Cascade short-circuit 的代價**：所有個體都 fatal 時，GA 完全沒有信號可學，這時看起來「演化跑完了」但其實啥都沒學到

---

## 附錄：相關 commit

- `14101c7` (4/26) — `tune(ga): introduce InitBounds for chromosome sampling`（這次的 InitBounds 收窄是錯誤方向，因為根因不在範圍而在單位）
- `eef026e` (4/27) — `feat(trades): add /trades page`（讓我們有工具看到大量成交）
- `XXXXXXX` (5/2) — `fix(strategy): SigmaFloor → SigmaFloorPct + hard risk guards`（本文檔對應 commit）
