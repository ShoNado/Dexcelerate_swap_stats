package model

import "time"

type Side string

const (
	Buy  Side = "buy"
	Sell Side = "sell"
)

type SwapEvent struct {
	EventID   string    `json:"event_id"`
	TokenID   string    `json:"token_id"`
	Amount    string    `json:"amount"`
	USD       float64   `json:"usd"`
	Side      Side      `json:"side"`
	BlockTime time.Time `json:"block_time"`
	ReceiveAt time.Time `json:"resice_at"`
}
