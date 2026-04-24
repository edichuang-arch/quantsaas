package instance

import (
	"context"
	"errors"
	"sync"

	"github.com/edi/quantsaas/internal/wsproto"
)

// TradeCommand 等同 wsproto.TradeCommand。
// 早期在 instance 包定义；为让 agent/saas 两端共用，已搬到 wsproto。
// 保留别名避免大范围改动。
type TradeCommand = wsproto.TradeCommand

// CommandSender 抽象接口：Tick 在产出 TradeCommand 后通过此接口下发。
// Phase 8 的 WebSocket Hub 会实现此接口；Phase 6 提供内存版 Fake 便于测试与开发。
//
// SendToUser 返回值：
//   - nil：指令已成功投递到 Agent 连接（或缓冲）
//   - ErrAgentNotConnected：用户没有在线的 Agent，调用方应 log 并跳过
//   - 其他 error：传输层错误，调用方决定是否重试
type CommandSender interface {
	SendToUser(ctx context.Context, userID uint, cmd TradeCommand) error
}

// ErrAgentNotConnected Agent 未连接，Tick 应跳过下发并等待下次。
var ErrAgentNotConnected = errors.New("agent not connected")

// InMemoryCommandSender 测试用实现：记录所有收到的指令到内存队列。
// 生产环境必须被 WebSocket Hub 替换。
type InMemoryCommandSender struct {
	mu           sync.Mutex
	queue        map[uint][]TradeCommand
	onlineUsers  map[uint]bool // true 表示视为"在线"
	failUsers    map[uint]error
}

// NewInMemoryCommandSender 构造一个 fake sender，默认所有用户离线。
func NewInMemoryCommandSender() *InMemoryCommandSender {
	return &InMemoryCommandSender{
		queue:       map[uint][]TradeCommand{},
		onlineUsers: map[uint]bool{},
		failUsers:   map[uint]error{},
	}
}

// MarkOnline 将指定用户标记为"Agent 已连接"。
func (s *InMemoryCommandSender) MarkOnline(userID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onlineUsers[userID] = true
	delete(s.failUsers, userID)
}

// MarkOffline 将指定用户标记为离线（后续 SendToUser 返回 ErrAgentNotConnected）。
func (s *InMemoryCommandSender) MarkOffline(userID uint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.onlineUsers, userID)
}

// InjectFailure 让指定用户的下一次 SendToUser 返回自定义错误（测试用）。
func (s *InMemoryCommandSender) InjectFailure(userID uint, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failUsers[userID] = err
}

// SendToUser 实现 CommandSender。
func (s *InMemoryCommandSender) SendToUser(ctx context.Context, userID uint, cmd TradeCommand) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := s.failUsers[userID]; ok {
		delete(s.failUsers, userID) // 单次失败注入
		return err
	}
	if !s.onlineUsers[userID] {
		return ErrAgentNotConnected
	}
	s.queue[userID] = append(s.queue[userID], cmd)
	return nil
}

// Sent 返回某用户收到的全部指令列表（测试断言用）。
func (s *InMemoryCommandSender) Sent(userID uint) []TradeCommand {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]TradeCommand, len(s.queue[userID]))
	copy(out, s.queue[userID])
	return out
}

// Reset 清空所有记录。
func (s *InMemoryCommandSender) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queue = map[uint][]TradeCommand{}
	s.failUsers = map[uint]error{}
}
