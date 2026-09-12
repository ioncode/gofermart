package domain

import "time"

type OrderStatus string

const (
	StatusNew        OrderStatus = "NEW"
	StatusProcessing OrderStatus = "PROCESSING"
	StatusInvalid    OrderStatus = "INVALID"
	StatusProcessed  OrderStatus = "PROCESSED"
)

type Order struct {
	ID         string      `json:"number"`
	UserID     string      `json:"-"`
	Status     OrderStatus `json:"status"`
	Accrual    float64     `json:"accrual,omitempty"` // omitempty, так как для NEW/PROCESSING поля может не быть
	UploadedAt time.Time   `json:"uploaded_at"`
}
