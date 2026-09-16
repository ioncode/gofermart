package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOrderStatus_IsFinal(t *testing.T) {
	tests := []struct {
		name   string
		status OrderStatus
		want   bool
	}{
		{"PROCESSED is final", StatusProcessed, true},
		{"INVALID is final", StatusInvalid, true},
		{"NEW is not final", StatusNew, false},
		{"PROCESSING is not final", StatusProcessing, false},
		{"Custom unknown status is not final", OrderStatus("UNKNOWN"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.IsFinal())
		})
	}
}

func TestOrderStatus_IsUnprocessed(t *testing.T) {
	tests := []struct {
		name   string
		status OrderStatus
		want   bool
	}{
		{"NEW is unprocessed", StatusNew, true},
		{"PROCESSING is unprocessed", StatusProcessing, true},
		{"PROCESSED is processed", StatusProcessed, false},
		{"INVALID is processed", StatusInvalid, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.IsUnprocessed())
		})
	}
}

func TestAccrualStatus_IsFinal(t *testing.T) {
	tests := []struct {
		name   string
		status AccrualStatus
		want   bool
	}{
		{"Accrual PROCESSED is final", AccrualProcessed, true},
		{"Accrual INVALID is final", AccrualInvalid, true},
		{"Accrual REGISTERED is not final", AccrualRegistered, false},
		{"Accrual PROCESSING is not final", AccrualProcessing, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.status.IsFinal())
		})
	}
}
