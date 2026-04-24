package store

import (
	"time"

	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// 本文件是系统 schema 的唯一真源（铁律 #6：GORM Code-First）。
// 任何 schema 变更通过修改本文件的 struct + AutoMigrate 同步，禁止手写 SQL migration。

// ----- 用户与认证 -------------------------------------------------------------

// SubscriptionPlan 枚举用户订阅计划，用于实例创建时的配额守门。
type SubscriptionPlan string

const (
	PlanFree       SubscriptionPlan = "free"
	PlanStarter    SubscriptionPlan = "starter"
	PlanPro        SubscriptionPlan = "pro"
	PlanEnterprise SubscriptionPlan = "enterprise"
)

// User 用户账户。email 为登录凭证；PasswordHash 必须用 bcrypt 或同等算法。
type User struct {
	ID           uint             `gorm:"primaryKey"`
	Email        string           `gorm:"uniqueIndex;size:255;not null"`
	PasswordHash string           `gorm:"size:255;not null"`
	Plan         SubscriptionPlan `gorm:"size:32;not null;default:'free'"`
	MaxInstances int              `gorm:"not null;default:1"` // 当前 plan 允许的最大实例数
	CreatedAt    time.Time
	UpdatedAt    time.Time
	DeletedAt    gorm.DeletedAt `gorm:"index"`
}

// ----- 策略模板与实例 ---------------------------------------------------------

// StrategyTemplate 策略"图纸"，不绑定用户，所有实例从模板派生。
type StrategyTemplate struct {
	ID          uint           `gorm:"primaryKey"`
	StrategyID  string         `gorm:"uniqueIndex:idx_tpl_sid_ver;size:64;not null"` // 如 "sigmoid-btc"
	Name        string         `gorm:"size:128;not null"`
	Version     string         `gorm:"uniqueIndex:idx_tpl_sid_ver;size:32;not null"`
	Description string         `gorm:"type:text"`
	IsSpot      bool           `gorm:"not null;default:true"`
	Exchange    string         `gorm:"size:32;not null"` // "binance"
	Symbol      string         `gorm:"size:32;not null"` // "BTCUSDT"
	Manifest    datatypes.JSON // 完整元数据：支持的 feature、默认参数、UI 展示色等
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// InstanceStatus 实例状态机枚举。
type InstanceStatus string

const (
	InstRunning InstanceStatus = "RUNNING" // cron 会扫描并推进 Step()
	InstStopped InstanceStatus = "STOPPED" // 人工暂停或初始未启动
	InstError   InstanceStatus = "ERROR"   // 连续 tick 失败，冻结等待人工排查
)

// StrategyInstance 实例 = 策略模板 + 交易标的 + 资金配额 + 用户授权。
type StrategyInstance struct {
	ID         uint           `gorm:"primaryKey"`
	UserID     uint             `gorm:"index;not null"`
	User       User             `gorm:"foreignKey:UserID" json:"-"`
	TemplateID uint             `gorm:"index;not null"`
	Template   StrategyTemplate `gorm:"foreignKey:TemplateID" json:"-"`
	Name       string         `gorm:"size:128;not null"` // 用户自定义实例名
	Symbol     string         `gorm:"size:32;not null"`
	Status     InstanceStatus `gorm:"size:16;not null;default:'STOPPED';index"`
	// 初始资金配额 USDT（用户创建时填写，仅作记录与配额校验用）。
	InitialCapitalUSDT float64 `gorm:"type:numeric(20,8);not null"`
	// 月度注资计划 USDT，可为 0。
	MonthlyInjectUSDT float64 `gorm:"type:numeric(20,8);not null;default:0"`
	// 绑定的冠军基因 snapshot；运行时从 Redis/DB 读最新 champion。
	ChampionGenomeID *uint `gorm:"index"`
	// 最后一次成功推进的 bar 时间戳（毫秒），用于 cron 幂等桶去重。
	LastProcessedBarTime int64 `gorm:"not null;default:0"`
	StartedAt            *time.Time
	StoppedAt            *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
	DeletedAt            gorm.DeletedAt `gorm:"index"`
}

// ----- 账户状态快照 -----------------------------------------------------------

// PortfolioState 实例的"真实账户快照"——由 Agent 上报的 DeltaReport 维护。
// 一个实例一条记录。
type PortfolioState struct {
	ID                 uint      `gorm:"primaryKey"`
	InstanceID         uint      `gorm:"uniqueIndex;not null"`
	USDTBalance        float64   `gorm:"type:numeric(20,8);not null;default:0"`
	USDTFrozen         float64   `gorm:"type:numeric(20,8);not null;default:0"`
	DeadStackAsset     float64   `gorm:"type:numeric(30,12);not null;default:0"` // 宏观底仓（不含 ColdSealed）
	FloatStackAsset    float64   `gorm:"type:numeric(30,12);not null;default:0"` // 微观浮动仓
	ColdSealedAsset    float64   `gorm:"type:numeric(30,12);not null;default:0"` // 冷封存
	LastPriceUSDT      float64   `gorm:"type:numeric(20,8);not null;default:0"`
	TotalEquity        float64   `gorm:"type:numeric(20,8);not null;default:0"`
	UpdatedAt          time.Time
}

// RuntimeState 策略内部运行时快照（例如上次决策时间、EMA 缓存等）。
// Content 由策略模块自行序列化/反序列化，SaaS 侧只做透明存储。
type RuntimeState struct {
	ID         uint           `gorm:"primaryKey"`
	InstanceID uint           `gorm:"uniqueIndex;not null"`
	Content    datatypes.JSON // 策略自行定义结构
	UpdatedAt  time.Time
}

// ----- 仓位 Lot 与成交记录 --------------------------------------------------

// LotType 区分 lot 的语义来源。
type LotType string

const (
	LotDeadStack  LotType = "DEAD_STACK" // 宏观定投买入，只进不出（除非软/硬释放）
	LotFloating   LotType = "FLOATING"   // 微观浮动仓，可买卖
	LotColdSealed LotType = "COLD_SEALED" // 冷封存，永不释放
)

// SpotLot 仓位 lot 记录。微观卖出时按 FIFO 从 FLOATING lot 扣减。
// 宏观释放时将 DEAD_STACK lot 转为 FLOATING（保留原始 CostPrice）。
type SpotLot struct {
	ID           uint      `gorm:"primaryKey"`
	InstanceID   uint      `gorm:"index;not null"`
	LotType      LotType   `gorm:"size:16;not null;index"`
	Amount       float64   `gorm:"type:numeric(30,12);not null"`
	CostPrice    float64   `gorm:"type:numeric(20,8);not null"`
	IsColdSealed bool      `gorm:"not null;default:false"` // 冷封存标志，任何情况下不可释放
	CreatedAt    time.Time `gorm:"index"`
}

// TradeRecord 已落袋的成交记录，用于报表与 PnL 计算。
type TradeRecord struct {
	ID            uint      `gorm:"primaryKey"`
	InstanceID    uint      `gorm:"index;not null"`
	ClientOrderID string    `gorm:"uniqueIndex;size:64;not null"`
	Action        string    `gorm:"size:8;not null"`  // BUY / SELL
	Engine        string    `gorm:"size:8;not null"`  // MACRO / MICRO
	Symbol        string    `gorm:"size:32;not null"`
	LotType       LotType   `gorm:"size:16;not null"`
	FilledQty     float64   `gorm:"type:numeric(30,12);not null"`
	FilledPrice   float64   `gorm:"type:numeric(20,8);not null"`
	FilledUSDT    float64   `gorm:"type:numeric(20,8);not null"`
	Fee           float64   `gorm:"type:numeric(20,8);not null"`
	FeeAsset      string    `gorm:"size:16"`
	CreatedAt     time.Time `gorm:"index"`
}

// SpotExecutionStatus 原始 execution 的状态机。
type SpotExecutionStatus string

const (
	ExecPending SpotExecutionStatus = "pending" // SaaS 下发后，等待 Agent 回报
	ExecFilled  SpotExecutionStatus = "filled"  // Agent 上报成交
	ExecFailed  SpotExecutionStatus = "failed"  // Agent 执行失败或拒单
)

// SpotExecution SaaS 发出 TradeCommand 的完整记录。
// 下发时 status=pending；收到 DeltaReport 后更新为 filled/failed。
type SpotExecution struct {
	ID            uint                `gorm:"primaryKey"`
	InstanceID    uint                `gorm:"index;not null"`
	ClientOrderID string              `gorm:"uniqueIndex;size:64;not null"`
	Action        string              `gorm:"size:8;not null"`
	Engine        string              `gorm:"size:8;not null"`
	Symbol        string              `gorm:"size:32;not null"`
	LotType       LotType             `gorm:"size:16;not null"`
	AmountUSDT    float64             `gorm:"type:numeric(20,8)"` // 买入时使用
	QtyAsset      float64             `gorm:"type:numeric(30,12)"` // 卖出时使用
	Status        SpotExecutionStatus `gorm:"size:16;not null;index"`
	FilledQty     float64             `gorm:"type:numeric(30,12)"`
	FilledPrice   float64             `gorm:"type:numeric(20,8)"`
	Fee           float64             `gorm:"type:numeric(20,8)"`
	ErrorMessage  string              `gorm:"type:text"`
	SentAt        time.Time
	FilledAt      *time.Time
}

// ----- 审计 / 进化 / 市场数据 ------------------------------------------------

// AuditLog 通用审计日志，用于记录 dead→float 释放、champion 切换等关键事件。
type AuditLog struct {
	ID         uint           `gorm:"primaryKey"`
	InstanceID *uint          `gorm:"index"` // 可为空（系统级事件）
	UserID     *uint          `gorm:"index"`
	EventType  string         `gorm:"size:64;not null;index"`
	Payload    datatypes.JSON
	CreatedAt  time.Time      `gorm:"index"`
}

// GenomeRole 基因记录的三种角色。
type GenomeRole string

const (
	RoleChallenger GenomeRole = "challenger" // 进化产出，等待人工审批
	RoleChampion   GenomeRole = "champion"   // 当前活跃冠军，驱动实盘
	RoleRetired    GenomeRole = "retired"    // 历史冠军，归档只读
)

// GeneRecord 基因库。同一 StrategyID+Symbol 同时只能有一个 champion。
type GeneRecord struct {
	ID           uint           `gorm:"primaryKey"`
	StrategyID   string         `gorm:"index:idx_gene_sid_sym;size:64;not null"`
	Symbol       string         `gorm:"index:idx_gene_sid_sym;size:32;not null"`
	Role         GenomeRole     `gorm:"size:16;not null;index"`
	TaskID       *uint          `gorm:"index"` // 进化任务 ID，来源追溯
	ScoreTotal   float64        `gorm:"type:numeric(20,8);not null;default:0"`
	MaxDrawdown  float64        `gorm:"type:numeric(10,6);not null;default:0"`
	WindowScores datatypes.JSON // {"6m": 0.12, "2y": 0.21, ...}
	ParamPack    datatypes.JSON `gorm:"not null"` // {spawn_point, sigmoid_btc_config}
	ActivatedAt  *time.Time     // 成为 champion 的时间
	RetiredAt    *time.Time
	CreatedAt    time.Time      `gorm:"index"`
}

// EvolutionTaskStatus 进化任务状态。
type EvolutionTaskStatus string

const (
	TaskPending EvolutionTaskStatus = "pending"
	TaskRunning EvolutionTaskStatus = "running"
	TaskDone    EvolutionTaskStatus = "done"
	TaskFailed  EvolutionTaskStatus = "failed"
	TaskAborted EvolutionTaskStatus = "aborted"
)

// EvolutionTask 进化任务记录。同时只允许一个 RUNNING 任务。
type EvolutionTask struct {
	ID              uint                `gorm:"primaryKey"`
	StrategyID      string              `gorm:"size:64;not null;index"`
	Symbol          string              `gorm:"size:32;not null"`
	Status          EvolutionTaskStatus `gorm:"size:16;not null;index"`
	PopSize         int                 `gorm:"not null"`
	MaxGenerations  int                 `gorm:"not null"`
	CurrentGen      int                 `gorm:"not null;default:0"`
	BestScore       float64             `gorm:"type:numeric(20,8);not null;default:0"`
	Config          datatypes.JSON
	ErrorMessage    string              `gorm:"type:text"`
	StartedAt       time.Time           `gorm:"index"`
	FinishedAt      *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// KLine 历史 K 线。唯一索引在 (Symbol, Interval, OpenTime) 上。
// 用于 GA 回测和实盘 tick 读取。
type KLine struct {
	ID        uint    `gorm:"primaryKey"`
	Symbol    string  `gorm:"uniqueIndex:idx_kline;size:32;not null"`
	Interval  string  `gorm:"uniqueIndex:idx_kline;size:16;not null"` // "1m"/"5m"/"1h"/"1d"
	OpenTime  int64   `gorm:"uniqueIndex:idx_kline;not null"` // 毫秒
	CloseTime int64   `gorm:"not null"`
	Open      float64 `gorm:"type:numeric(20,8);not null"`
	High      float64 `gorm:"type:numeric(20,8);not null"`
	Low       float64 `gorm:"type:numeric(20,8);not null"`
	Close     float64 `gorm:"type:numeric(20,8);not null"`
	Volume    float64 `gorm:"type:numeric(30,12);not null"`
}

// AllModels 返回所有需要 AutoMigrate 的模型指针列表，供 db.go 使用。
func AllModels() []any {
	return []any{
		&User{},
		&StrategyTemplate{},
		&StrategyInstance{},
		&PortfolioState{},
		&RuntimeState{},
		&SpotLot{},
		&TradeRecord{},
		&SpotExecution{},
		&AuditLog{},
		&GeneRecord{},
		&EvolutionTask{},
		&KLine{},
	}
}
