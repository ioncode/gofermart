package accrual

import (
	"sync/atomic"
	"time"
)

// Throttler представляет собой потокобезопасный "стоп-кран", инкапсулирующий состояние экстренной паузы при ошибках 429.
type Throttler struct {
	blockedUntil atomic.Pointer[time.Time]
}

func (t *Throttler) IsBlocked() bool {
	until := t.blockedUntil.Load()
	return until != nil && time.Now().Before(*until)
}

func (t *Throttler) Block(duration time.Duration) {
	banTime := time.Now().Add(duration)
	t.blockedUntil.Store(&banTime)
}
