package telegraminitdata

import (
	"fmt"
	"os"
)

type End string

const (
	Buyer    End = "buyer"
	Merchant End = "merchant"
	Driver   End = "driver"
)

type Bot struct {
	Label string
	Token string
}

type PoolVerifier struct {
	Validator Validator
	Pools     map[End][]Bot
}

type PoolResult struct {
	Data       ValidatedData
	HitBot     string
	TriedCount int
}

func (p PoolVerifier) Verify(end End, raw string) (PoolResult, error) {
	bots := p.Pools[end]
	if len(bots) == 0 {
		return PoolResult{}, ErrInvalidInitData
	}
	for i, bot := range bots {
		data, err := p.Validator.Validate(raw, bot.Token)
		if err == nil {
			label := bot.Label
			if label == "" {
				label = fmt.Sprintf("bot-%d", i+1)
			}
			return PoolResult{Data: data, HitBot: string(end) + "/" + label, TriedCount: i + 1}, nil
		}
	}
	return PoolResult{TriedCount: len(bots)}, ErrInvalidInitData
}

func LoadPoolsFromEnv() map[End][]Bot {
	type envBot struct {
		end   End
		label string
		name  string
	}
	entries := []envBot{
		{Buyer, "primary", "POC_TG_BUYER_PRIMARY_TOKEN"},
		{Buyer, "standby", "POC_TG_BUYER_STANDBY_TOKEN"},
		{Merchant, "primary", "POC_TG_MERCHANT_PRIMARY_TOKEN"},
		{Merchant, "standby", "POC_TG_MERCHANT_STANDBY_TOKEN"},
		{Driver, "primary", "POC_TG_DRIVER_PRIMARY_TOKEN"},
		{Driver, "standby", "POC_TG_DRIVER_STANDBY_TOKEN"},
	}
	pools := make(map[End][]Bot)
	for _, entry := range entries {
		if token := os.Getenv(entry.name); token != "" {
			pools[entry.end] = append(pools[entry.end], Bot{Label: entry.label, Token: token})
		}
	}
	return pools
}
