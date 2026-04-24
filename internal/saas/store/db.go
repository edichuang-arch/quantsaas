package store

import (
	"fmt"
	"time"

	"github.com/edi/quantsaas/internal/saas/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// DB 是 GORM 连接的轻薄封装，供业务层注入。
// 之所以封装一层：后续可加 Tx helper、指标埋点，不必到处改签名。
type DB struct {
	*gorm.DB
}

// NewDB 打开 Postgres 连接，并对全部模型执行 AutoMigrate（铁律 #6）。
// 任何 schema 变更都由 Go struct 驱动，不写 SQL migration。
func NewDB(cfg config.DatabaseConfig) (*DB, error) {
	gormCfg := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
		NowFunc: func() time.Time {
			// 统一使用 UTC，避免各端时区混乱；展示层再转本地时区。
			return time.Now().UTC()
		},
	}
	raw, err := gorm.Open(postgres.Open(cfg.DSN()), gormCfg)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	sqlDB, err := raw.DB()
	if err != nil {
		return nil, fmt.Errorf("get underlying *sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(40)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	if err := raw.AutoMigrate(AllModels()...); err != nil {
		return nil, fmt.Errorf("auto migrate: %w", err)
	}
	return &DB{DB: raw}, nil
}

// Close 关闭底层连接池，用于优雅停机。
func (d *DB) Close() error {
	sqlDB, err := d.DB.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
