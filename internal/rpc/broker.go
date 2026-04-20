package rpc

import (
	"context"
	"errors"
	"strings"
	"sync"

	luruntime "github.com/lutefd/luc/internal/runtime"
)

var errBrokerClosed = errors.New("rpc ui broker is closed")

type brokerReply struct {
	result luruntime.UIResult
	err    error
}

type Broker struct {
	mu      sync.Mutex
	pending map[string]chan brokerReply
	closed  bool
}

func NewBroker() *Broker {
	return &Broker{pending: map[string]chan brokerReply{}}
}

func (b *Broker) Publish(action luruntime.UIAction) error {
	return nil
}

func (b *Broker) Request(ctx context.Context, action luruntime.UIAction) (luruntime.UIResult, error) {
	actionID := strings.TrimSpace(action.ID)
	if actionID == "" {
		return luruntime.UIResult{}, errors.New("rpc ui action is missing id")
	}

	replyCh := make(chan brokerReply, 1)

	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return luruntime.UIResult{}, errBrokerClosed
	}
	if _, exists := b.pending[actionID]; exists {
		b.mu.Unlock()
		return luruntime.UIResult{}, errors.New("rpc ui action is already pending: " + actionID)
	}
	b.pending[actionID] = replyCh
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pending, actionID)
		b.mu.Unlock()
	}()

	select {
	case reply := <-replyCh:
		return reply.result, reply.err
	case <-ctx.Done():
		return luruntime.UIResult{}, ctx.Err()
	}
}

func (b *Broker) Respond(actionID string, result luruntime.UIResult) error {
	actionID = strings.TrimSpace(actionID)
	if actionID == "" {
		return errors.New("action_id is required")
	}

	b.mu.Lock()
	replyCh, ok := b.pending[actionID]
	b.mu.Unlock()
	if !ok {
		return errors.New("no pending ui action for id " + actionID)
	}

	if strings.TrimSpace(result.ActionID) == "" {
		result.ActionID = actionID
	}
	replyCh <- brokerReply{result: result}
	return nil
}

func (b *Broker) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return
	}
	b.closed = true
	for actionID, replyCh := range b.pending {
		replyCh <- brokerReply{
			result: luruntime.UIResult{ActionID: actionID},
			err:    errBrokerClosed,
		}
		delete(b.pending, actionID)
	}
}
