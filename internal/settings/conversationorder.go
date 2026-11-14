package settings

import (
	"context"
	"errors"
)

// KeyConversationOrder is the order the sheets list a conversation in, the one the user
// last chose, stored as one of the ConversationOrder choices: the window's memory, which a
// backup never holds.
const KeyConversationOrder = "ui.conversationOrder"

// ConversationOrder is the order the issue sheet and the review sheet list a conversation's
// entries in, under its description.
type ConversationOrder string

// The conversation's orders.
const (
	ConversationOldest ConversationOrder = "oldest"
	ConversationNewest ConversationOrder = "newest"
)

// DefaultConversationOrder is the order of a fresh database: oldest first, as GitHub lists
// a conversation.
const DefaultConversationOrder = ConversationOldest

// ErrInvalidConversationOrder is returned for a conversation order that is not one of the
// choices.
var ErrInvalidConversationOrder = errors.New("unknown conversation order")

// conversationOrders is the conversation order choice, falling back to
// DefaultConversationOrder for a stored order that is unset or no longer a choice.
func conversationOrders() choice[ConversationOrder] {
	return choice[ConversationOrder]{
		values:   []ConversationOrder{ConversationOldest, ConversationNewest},
		fallback: DefaultConversationOrder, invalid: ErrInvalidConversationOrder,
	}
}

// SetConversationOrder stores the order the sheets list a conversation in,
// ConversationOldest or ConversationNewest, and returns the settings with it.
func (s *Service) SetConversationOrder(ctx context.Context, order ConversationOrder) (Settings, error) {
	if err := conversationOrders().check(order); err != nil {
		return Settings{}, err
	}

	return s.save(ctx, setting(KeyConversationOrder, string(order)))
}
