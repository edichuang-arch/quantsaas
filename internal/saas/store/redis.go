package store

import (
	"context"
	"fmt"
	"time"

	"github.com/edi/quantsaas/internal/saas/config"
	"github.com/redis/go-redis/v9"
)

// Redis 用途仅限缓存（铁律 #6）：
//   - 冠军基因缓存 key: champion:{strategyID}:{symbol}
//   - 会话缓存（如需要）
// 绝对不用作信号传递通道或分布式锁以外的同步原语。
type Redis struct {
	client *redis.Client
}

// NewRedis 建立 Redis 连接并 Ping 验证。
func NewRedis(cfg config.RedisConfig) (*Redis, error) {
	c := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		PoolSize:     20,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("ping redis: %w", err)
	}
	return &Redis{client: c}, nil
}

// Get 读取字符串，key 不存在返回 ("", nil) 而非错误。
func (r *Redis) Get(ctx context.Context, key string) (string, error) {
	v, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", nil
	}
	return v, err
}

// Set 写入字符串，ttl <= 0 表示永不过期。
func (r *Redis) Set(ctx context.Context, key, value string, ttl time.Duration) error {
	return r.client.Set(ctx, key, value, ttl).Err()
}

// Del 删除 key，忽略 key 不存在的情况。
func (r *Redis) Del(ctx context.Context, keys ...string) error {
	return r.client.Del(ctx, keys...).Err()
}

// Close 关闭连接池。
func (r *Redis) Close() error { return r.client.Close() }

// Raw 返回底层 client，供需要高级操作的模块使用（例如 Pipeline）。
func (r *Redis) Raw() *redis.Client { return r.client }
