package model

import "time"

type Side string

const (
	Buy  Side = "buy"
	Sell Side = "sell"
)

var Sides = []Side{Buy, Sell}

type SwapEvent struct {
	EventID    string    `json:"event_id"`
	TokenID    string    `json:"token_id"`
	Amount     float64   `json:"amount"`
	USD        float64   `json:"usd"`
	Side       Side      `json:"side"`
	Rate       float64   `json:"rate"` //usd per token
	CreatedAt  time.Time `json:"created_at"`
	ExecutedAt time.Time `json:"executed_at"`
}

type Bucket struct {
	Count    uint64  `json:"transaction counts"`
	USD      float64 `json:"total usd volume"`
	Quantity float64 `json:"total token volume"`
}

// Stats: if more various time Interval needed we can add here or create custom one
type Stats struct {
	Token          string `json:"token"`
	BucketMinutes5 Bucket `json:"bucket_minutes_5"`
	BucketHours1   Bucket `json:"bucket_hours_1"`
	BucketHours24  Bucket `json:"bucket_hours_24"`
	UpdatedAt      int64  `json:"updated_at"`
}
