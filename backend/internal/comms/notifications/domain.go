package notifications

import (
	"errors"
	"fmt"
	"strings"
)

type End string

const (
	Buyer    End = "buyer"
	Merchant End = "merchant"
)

type EventType string

const (
	OrderCreated         EventType = "order.created"
	PaymentConfirmed     EventType = "payment.confirmed"
	OrderShipped         EventType = "order.shipped"
	OrderDelivered       EventType = "order.delivered"
	ProductModerated     EventType = "product.moderated"
	WarehouseReceived    EventType = "warehouse.received"
	AccountStatusChanged EventType = "account.status_changed"
	AgreementUpdated     EventType = "agreement.updated"
	WithdrawalSettled    EventType = "withdrawal.settled"
	SettlementSettled    EventType = "settlement.settled"
)

type Event struct {
	ID        string
	AccountID int64
	End       End
	Type      EventType
	Title     map[string]string
	Body      map[string]string
}

func (e Event) Validate() error {
	if strings.TrimSpace(e.ID) == "" || e.AccountID <= 0 || !e.End.Valid() || !e.Type.Valid() {
		return errors.New("notifications: invalid event")
	}
	return nil
}

func (e End) Valid() bool { return e == Buyer || e == Merchant }

// RecipientEventKey is the idempotency key for one event delivered to one
// account/end. The same event may legitimately have one buyer and one merchant
// recipient, while duplicate writes for either recipient collapse to one row.
func RecipientEventKey(eventID string, accountID int64, end End) string {
	return fmt.Sprintf("%s:%d:%s", eventID, accountID, end)
}

func (t EventType) Valid() bool {
	switch t {
	case OrderCreated, PaymentConfirmed, OrderShipped, OrderDelivered,
		ProductModerated, WarehouseReceived, AccountStatusChanged,
		AgreementUpdated, WithdrawalSettled, SettlementSettled:
		return true
	default:
		return false
	}
}

type Bot struct {
	ID     int64
	End    End
	Active bool
}

type DeliveryPlan struct {
	InApp         bool
	TelegramBotID int64
	SkipReason    string
}

func Plan(event Event, bots []Bot, telegramEnabled bool) (DeliveryPlan, error) {
	if err := event.Validate(); err != nil {
		return DeliveryPlan{}, err
	}
	plan := DeliveryPlan{InApp: true}
	if !telegramEnabled {
		plan.SkipReason = "disabled_by_recipient"
		return plan, nil
	}
	for _, bot := range bots {
		if bot.End == event.End && bot.Active {
			plan.TelegramBotID = bot.ID
			return plan, nil
		}
	}
	plan.SkipReason = "no_available_bot"
	return plan, nil
}
