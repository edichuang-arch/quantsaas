package ga

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/edi/quantsaas/internal/saas/store"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// GenomeStore 封装 GeneRecord 表的查询与写入，以及冠军缓存。
//
// 核心职责：
//   - 加载指定策略+标的的精英基因（champion > challenger > retired 排序，按 score 降序）
//   - 写入新的 challenger（Epoch 结束时）
//   - Promote：challenger → champion，旧 champion → retired（DB 事务）
//   - 冠军缓存（Redis key: champion:{strategyID}:{symbol}）
type GenomeStore struct {
	DB    *store.DB
	Redis *store.Redis
}

// NewGenomeStore 构造一个 store。Redis 可为 nil（测试用）。
func NewGenomeStore(db *store.DB, r *store.Redis) *GenomeStore {
	return &GenomeStore{DB: db, Redis: r}
}

// LoadElites 读取指定策略+标的的精英基因包（按 score 降序、最多 limit 条）。
// 实现 engine.ElitesLoader 接口。
func (s *GenomeStore) LoadElites(ctx context.Context, strategyID, symbol string, limit int) ([]Gene, error) {
	if s.DB == nil {
		return nil, errors.New("db nil")
	}
	if limit <= 0 {
		limit = 100
	}
	var records []store.GeneRecord
	err := s.DB.WithContext(ctx).
		Where("strategy_id = ? AND symbol = ? AND role IN ?", strategyID, symbol, []store.GenomeRole{store.RoleChampion, store.RoleChallenger}).
		Order("score_total DESC").
		Limit(limit).
		Find(&records).Error
	if err != nil {
		return nil, fmt.Errorf("load elites: %w", err)
	}
	genes := make([]Gene, 0, len(records))
	ev := NewSigmoidBTCEvolvable() // 当前仅支持 sigmoid-btc；未来按 strategyID 分派
	for _, r := range records {
		genes = append(genes, ev.DecodeElite(r.ParamPack))
	}
	return genes, nil
}

// SaveChallenger 写入一个 challenger 记录，不修改任何现有 champion。
// 返回新记录的 ID。
func (s *GenomeStore) SaveChallenger(
	ctx context.Context,
	strategyID, symbol string,
	taskID *uint,
	paramPack []byte,
	scoreTotal, maxDD float64,
	windowScores map[string]float64,
) (uint, error) {
	ws, err := json.Marshal(windowScores)
	if err != nil {
		return 0, fmt.Errorf("marshal window scores: %w", err)
	}
	rec := store.GeneRecord{
		StrategyID:   strategyID,
		Symbol:       symbol,
		Role:         store.RoleChallenger,
		TaskID:       taskID,
		ScoreTotal:   scoreTotal,
		MaxDrawdown:  maxDD,
		WindowScores: datatypes.JSON(ws),
		ParamPack:    datatypes.JSON(paramPack),
	}
	if err := s.DB.WithContext(ctx).Create(&rec).Error; err != nil {
		return 0, fmt.Errorf("save challenger: %w", err)
	}
	return rec.ID, nil
}

// Promote 将指定 challenger 晋升为 champion（事务内）：
//   1. 当前 champion → retired（记录 RetiredAt）
//   2. challenger → champion（记录 ActivatedAt）
//   3. 刷新 Redis champion 缓存（Delete，下次 tick 重新 Load）
func (s *GenomeStore) Promote(ctx context.Context, challengerID uint) error {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var chal store.GeneRecord
		if err := tx.First(&chal, challengerID).Error; err != nil {
			return fmt.Errorf("load challenger: %w", err)
		}
		if chal.Role != store.RoleChallenger {
			return fmt.Errorf("gene %d is not a challenger (role=%s)", challengerID, chal.Role)
		}
		now := time.Now().UTC()
		// 退役当前 champion
		if err := tx.Model(&store.GeneRecord{}).
			Where("strategy_id = ? AND symbol = ? AND role = ?", chal.StrategyID, chal.Symbol, store.RoleChampion).
			Updates(map[string]any{
				"role":       store.RoleRetired,
				"retired_at": &now,
			}).Error; err != nil {
			return fmt.Errorf("retire old champion: %w", err)
		}
		// 晋升
		if err := tx.Model(&chal).Updates(map[string]any{
			"role":         store.RoleChampion,
			"activated_at": &now,
		}).Error; err != nil {
			return fmt.Errorf("activate new champion: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	// 缓存失效
	if s.Redis != nil {
		_ = s.Redis.Del(ctx, championKey("sigmoid-btc", ""))
	}
	return nil
}

// LoadChampion 读取当前 champion（Redis cache first，miss 时 fallback DB）。
// 若没有 champion，返回 (nil, nil)；调用方应回退 DefaultSeedChromosome。
func (s *GenomeStore) LoadChampion(ctx context.Context, strategyID, symbol string) (*store.GeneRecord, error) {
	// Redis 命中
	if s.Redis != nil {
		key := championKey(strategyID, symbol)
		raw, err := s.Redis.Get(ctx, key)
		if err == nil && raw != "" {
			var rec store.GeneRecord
			if err := json.Unmarshal([]byte(raw), &rec); err == nil {
				return &rec, nil
			}
		}
	}
	// DB 查询
	var rec store.GeneRecord
	err := s.DB.WithContext(ctx).
		Where("strategy_id = ? AND symbol = ? AND role = ?", strategyID, symbol, store.RoleChampion).
		First(&rec).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	// 回写缓存（TTL 6 小时）
	if s.Redis != nil {
		if blob, err := json.Marshal(rec); err == nil {
			_ = s.Redis.Set(ctx, championKey(strategyID, symbol), string(blob), 6*time.Hour)
		}
	}
	return &rec, nil
}

// ListChallengers 列出指定策略最近的 challenger 记录（分数降序）。
func (s *GenomeStore) ListChallengers(ctx context.Context, strategyID, symbol string, limit int) ([]store.GeneRecord, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	var records []store.GeneRecord
	err := s.DB.WithContext(ctx).
		Where("strategy_id = ? AND symbol = ? AND role = ?", strategyID, symbol, store.RoleChallenger).
		Order("score_total DESC").
		Limit(limit).
		Find(&records).Error
	return records, err
}

func championKey(strategyID, symbol string) string {
	if symbol == "" {
		return "champion:" + strategyID
	}
	return "champion:" + strategyID + ":" + symbol
}
