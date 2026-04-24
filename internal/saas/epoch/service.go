// Package epoch 提供进化任务的服务层。
//
// 职责：
//   - 限制同一时刻只允许一个任务运行（mutex + DB 查询双重保护）
//   - 拉取历史 K 线并构建 EvaluablePlan
//   - 异步启动 GA 引擎 goroutine
//   - 回调驱动更新 EvolutionTask 的进度（CurrentGen / BestScore）
//   - 结束时写 challenger 记录到 GeneRecord
//
// HTTP 层（api/handler_evolution.go）调用本包的 EpochService.Start。
package epoch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/edi/quantsaas/internal/quant"
	"github.com/edi/quantsaas/internal/saas/ga"
	"github.com/edi/quantsaas/internal/saas/store"
	"go.uber.org/zap"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// SpawnMode 进化任务的出生点抽样模式。
type SpawnMode string

const (
	SpawnInherit    SpawnMode = "inherit"     // 继承当前冠军或默认的出生点
	SpawnRandomOnce SpawnMode = "random_once" // 任务开始时抽样一次并冻结
	SpawnManual     SpawnMode = "manual"      // 请求体显式提供 spawn_point
)

// StartRequest 创建进化任务时的参数。
type StartRequest struct {
	StrategyID     string
	Symbol         string
	PopSize        int
	MaxGenerations int
	SpawnMode      SpawnMode
	SpawnPoint     *quant.SpawnPoint // SpawnManual 时使用
	InitialUSDT    float64
	MonthlyInject  float64
	LotStep        float64
	LotMin         float64
	WarmupDays     int
}

// Service 进化任务服务，持有 DB / Engine / GenomeStore / Logger。
type Service struct {
	DB      *store.DB
	Engine  *ga.Engine
	Genomes *ga.GenomeStore
	Log     *zap.Logger

	mu      sync.Mutex
	current *store.EvolutionTask
}

// NewService 构造 Service。
func NewService(db *store.DB, engine *ga.Engine, genomes *ga.GenomeStore, log *zap.Logger) *Service {
	if log == nil {
		log = zap.NewNop()
	}
	return &Service{DB: db, Engine: engine, Genomes: genomes, Log: log}
}

// Current 返回当前运行中的任务（只读副本）；无任务返回 nil。
func (s *Service) Current() *store.EvolutionTask {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.current == nil {
		return nil
	}
	c := *s.current
	return &c
}

// Start 创建并启动一个进化任务。
// 若已有运行中任务则返回 ErrTaskAlreadyRunning。
func (s *Service) Start(ctx context.Context, req StartRequest) (*store.EvolutionTask, error) {
	s.mu.Lock()
	if s.current != nil {
		s.mu.Unlock()
		return nil, ErrTaskAlreadyRunning
	}
	// 默认值
	if req.PopSize <= 0 {
		req.PopSize = ga.DefaultConfig.PopSize
	}
	if req.MaxGenerations <= 0 {
		req.MaxGenerations = ga.DefaultConfig.MaxGenerations
	}

	cfgBlob, _ := json.Marshal(req)
	task := &store.EvolutionTask{
		StrategyID:     req.StrategyID,
		Symbol:         req.Symbol,
		Status:         store.TaskRunning,
		PopSize:        req.PopSize,
		MaxGenerations: req.MaxGenerations,
		Config:         datatypes.JSON(cfgBlob),
		StartedAt:      time.Now().UTC(),
	}
	if err := s.DB.WithContext(ctx).Create(task).Error; err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("create task: %w", err)
	}
	s.current = task
	s.mu.Unlock()

	// 异步跑 GA
	go s.runTask(context.Background(), task, req)
	return task, nil
}

// runTask 执行 GA Epoch，结束后更新任务状态与写 challenger。
func (s *Service) runTask(ctx context.Context, task *store.EvolutionTask, req StartRequest) {
	defer func() {
		s.mu.Lock()
		s.current = nil
		s.mu.Unlock()
	}()

	bars, err := s.loadBars(ctx, req.Symbol)
	if err != nil {
		s.markFailed(task, fmt.Errorf("load bars: %w", err))
		return
	}
	if len(bars) == 0 {
		s.markFailed(task, errors.New("no historical bars available"))
		return
	}

	// 构建出生点
	spawn, err := s.resolveSpawn(ctx, req)
	if err != nil {
		s.markFailed(task, err)
		return
	}

	// 构建计划
	plan := ga.BuildEvaluablePlan(
		bars, req.Symbol, req.StrategyID,
		req.LotStep, req.LotMin, req.InitialUSDT, req.MonthlyInject,
		spawn, req.WarmupDays,
	)

	ec := ga.EpochConfig{
		PopSize:        req.PopSize,
		MaxGenerations: req.MaxGenerations,
		LotStep:        req.LotStep,
		LotMin:         req.LotMin,
		InitialUSDT:    req.InitialUSDT,
		MonthlyInject:  req.MonthlyInject,
		Pair:           req.Symbol,
		TemplateName:   req.StrategyID,
		SpawnPointOverride: spawn,
		OnProgress: func(gen int, bestScore, mutProb, mutScale float64) {
			_ = s.DB.Model(task).Updates(map[string]any{
				"current_gen": gen + 1,
				"best_score":  bestScore,
			}).Error
		},
	}

	result, err := s.Engine.RunEpoch(ctx, plan, ec)
	if err != nil {
		s.markFailed(task, err)
		return
	}

	// 写 challenger 记录
	ev := ga.NewSigmoidBTCEvolvable() // 目前只有 sigmoid-btc；未来按 StrategyID 分派
	paramPack, err := ev.EncodeResult(result.BestGene, spawn)
	if err != nil {
		s.markFailed(task, fmt.Errorf("encode result: %w", err))
		return
	}
	windowScores := map[string]float64{}
	for _, r := range result.BestEvalResult.Results {
		windowScores[r.Label] = r.Score
	}
	taskID := task.ID
	_, err = s.Genomes.SaveChallenger(ctx, req.StrategyID, req.Symbol, &taskID,
		paramPack, result.BestScore, result.BestEvalResult.MaxDrawdown, windowScores)
	if err != nil {
		s.markFailed(task, fmt.Errorf("save challenger: %w", err))
		return
	}

	now := time.Now().UTC()
	_ = s.DB.Model(task).Updates(map[string]any{
		"status":      store.TaskDone,
		"current_gen": result.Generations,
		"best_score":  result.BestScore,
		"finished_at": &now,
	}).Error
	s.Log.Info("epoch done",
		zap.Uint("task_id", task.ID),
		zap.Float64("best_score", result.BestScore),
		zap.Int("cache_hits", result.CacheHits))
}

func (s *Service) resolveSpawn(ctx context.Context, req StartRequest) (*quant.SpawnPoint, error) {
	switch req.SpawnMode {
	case SpawnManual:
		if req.SpawnPoint == nil {
			return nil, errors.New("spawn_mode=manual requires spawn_point")
		}
		sp := *req.SpawnPoint
		return &sp, nil
	case SpawnRandomOnce:
		sp := quant.DefaultSpawnPoint
		// 当前未实现随机采样；预留此分支为占位，与文档保持一致
		return &sp, nil
	case SpawnInherit, "":
		champ, err := s.Genomes.LoadChampion(ctx, req.StrategyID, req.Symbol)
		if err != nil {
			return nil, fmt.Errorf("load champion: %w", err)
		}
		if champ == nil {
			sp := quant.DefaultSpawnPoint
			return &sp, nil
		}
		_, sp := quant.DecodeParamPack(champ.ParamPack)
		return &sp, nil
	default:
		return nil, fmt.Errorf("unknown spawn_mode: %s", req.SpawnMode)
	}
}

// loadBars 从 DB 拉取指定 symbol 的全量 5m K 线（方案 Y 的默认聚合周期）。
// 按 OpenTime 升序。
func (s *Service) loadBars(ctx context.Context, symbol string) ([]quant.Bar, error) {
	var rows []store.KLine
	err := s.DB.WithContext(ctx).
		Where("symbol = ? AND interval = ?", symbol, "5m").
		Order("open_time ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	bars := make([]quant.Bar, len(rows))
	for i, r := range rows {
		bars[i] = quant.Bar{
			OpenTime:  r.OpenTime,
			CloseTime: r.CloseTime,
			Open:      r.Open,
			High:      r.High,
			Low:       r.Low,
			Close:     r.Close,
			Volume:    r.Volume,
		}
	}
	return bars, nil
}

func (s *Service) markFailed(task *store.EvolutionTask, cause error) {
	s.Log.Error("epoch failed", zap.Uint("task_id", task.ID), zap.Error(cause))
	now := time.Now().UTC()
	_ = s.DB.Model(task).Updates(map[string]any{
		"status":        store.TaskFailed,
		"error_message": cause.Error(),
		"finished_at":   &now,
	}).Error
}

// ErrTaskAlreadyRunning 已有进化任务在运行。
var ErrTaskAlreadyRunning = errors.New("an evolution task is already running")

// 确保包导入 gorm（future-proof：transaction helper 可能需要）
var _ = gorm.ErrRecordNotFound
