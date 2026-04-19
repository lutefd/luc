package runtime

import (
	"context"
	"fmt"
	"strings"
)

type UIBroker interface {
	Publish(action UIAction) error
	Request(ctx context.Context, action UIAction) (UIResult, error)
}

type DefaultBroker struct {
	ApprovalsMode string
	Logf          func(format string, args ...any)
}

func NewDefaultBroker(approvalsMode string, logf func(format string, args ...any)) *DefaultBroker {
	return &DefaultBroker{
		ApprovalsMode: strings.TrimSpace(approvalsMode),
		Logf:          logf,
	}
}

func (b *DefaultBroker) Publish(action UIAction) error {
	if action.ID == "" {
		action.ID = "ui_action"
	}
	b.logf("ignoring non-blocking UI action %s (%s) in default broker", action.ID, action.Kind)
	if action.Blocking {
		_, err := b.Request(context.Background(), action)
		return err
	}
	return nil
}

func (b *DefaultBroker) Request(ctx context.Context, action UIAction) (UIResult, error) {
	if action.ID == "" {
		action.ID = "ui_action"
	}
	select {
	case <-ctx.Done():
		return UIResult{}, ctx.Err()
	default:
	}

	result := UIResult{
		ActionID: action.ID,
		Data:     map[string]any{},
	}

	if strings.EqualFold(action.Kind, "confirm.request") && strings.EqualFold(strings.TrimSpace(b.ApprovalsMode), "trusted") {
		result.Accepted = true
		if choice := primaryChoice(action.Options); choice != "" {
			result.ChoiceID = choice
		}
		b.logf("auto-accepted confirmation %s in trusted mode", action.ID)
		return result, nil
	}

	if action.Blocking {
		err := fmt.Errorf("blocking UI action %q (%s) requires an interactive broker", action.ID, action.Kind)
		b.logf(err.Error())
		return result, err
	}

	b.logf("ignoring UI request %s (%s) in default broker", action.ID, action.Kind)
	return result, nil
}

func (b *DefaultBroker) logf(format string, args ...any) {
	if b != nil && b.Logf != nil {
		b.Logf(format, args...)
	}
}

func primaryChoice(options []UIOption) string {
	for _, option := range options {
		if option.Primary && strings.TrimSpace(option.ID) != "" {
			return option.ID
		}
	}
	for _, option := range options {
		if strings.TrimSpace(option.ID) != "" {
			return option.ID
		}
	}
	return ""
}
