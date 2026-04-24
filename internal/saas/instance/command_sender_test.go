package instance

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInMemorySender_OfflineByDefault(t *testing.T) {
	s := NewInMemoryCommandSender()
	err := s.SendToUser(context.Background(), 1, TradeCommand{ClientOrderID: "x"})
	assert.ErrorIs(t, err, ErrAgentNotConnected)
}

func TestInMemorySender_OnlineAcceptsCommand(t *testing.T) {
	s := NewInMemoryCommandSender()
	s.MarkOnline(42)
	err := s.SendToUser(context.Background(), 42, TradeCommand{
		ClientOrderID: "oid-1",
		Action:        "BUY",
		Engine:        "MACRO",
	})
	assert.NoError(t, err)
	sent := s.Sent(42)
	assert.Len(t, sent, 1)
	assert.Equal(t, "oid-1", sent[0].ClientOrderID)
}

func TestInMemorySender_MarkOfflineRejects(t *testing.T) {
	s := NewInMemoryCommandSender()
	s.MarkOnline(1)
	s.MarkOffline(1)
	err := s.SendToUser(context.Background(), 1, TradeCommand{ClientOrderID: "x"})
	assert.ErrorIs(t, err, ErrAgentNotConnected)
}

func TestInMemorySender_InjectFailureOneShot(t *testing.T) {
	s := NewInMemoryCommandSender()
	s.MarkOnline(7)
	custom := errors.New("transport broken")
	s.InjectFailure(7, custom)

	err := s.SendToUser(context.Background(), 7, TradeCommand{ClientOrderID: "x"})
	assert.ErrorIs(t, err, custom)

	// 下次正常
	err = s.SendToUser(context.Background(), 7, TradeCommand{ClientOrderID: "y"})
	assert.NoError(t, err)
}

func TestInMemorySender_Reset(t *testing.T) {
	s := NewInMemoryCommandSender()
	s.MarkOnline(3)
	_ = s.SendToUser(context.Background(), 3, TradeCommand{ClientOrderID: "x"})
	s.Reset()
	assert.Len(t, s.Sent(3), 0)
}

func TestInMemorySender_MultipleUsersIsolated(t *testing.T) {
	s := NewInMemoryCommandSender()
	s.MarkOnline(1)
	s.MarkOnline(2)
	_ = s.SendToUser(context.Background(), 1, TradeCommand{ClientOrderID: "a"})
	_ = s.SendToUser(context.Background(), 2, TradeCommand{ClientOrderID: "b"})
	assert.Len(t, s.Sent(1), 1)
	assert.Len(t, s.Sent(2), 1)
	assert.Equal(t, "a", s.Sent(1)[0].ClientOrderID)
	assert.Equal(t, "b", s.Sent(2)[0].ClientOrderID)
}
