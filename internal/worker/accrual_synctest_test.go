package worker

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
)

func TestAccrualWorker_HandleBackoff_VirtualTime(t *testing.T) {
	logger := uzerolog.NewZerologAdapter(zerolog.New(nil)) // Выключаем логирование, чтобы не мусорить в аллокации
	worker := NewAccrualWorker(nil, nil, "", logger)

	synctest.Test(t, func(t *testing.T) {
		// Инициализируем контекст приложения строго внутри замыкания для предотвращения ошибки fatal error: close of synctest channel from outside bubble
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		backoffDuration := 5 * time.Second
		startVirtualTime := time.Now()

		// Имитируем сон воркера. Виртуальные часы сдвинутся мгновенно,
		// как только горутина заблокируется в ожидании таймера.
		worker.handleBackoff(ctx, backoffDuration)

		endVirtualTime := time.Now()
		elapsedVirtualTime := endVirtualTime.Sub(startVirtualTime)

		// Проверяем виртуальный сдвиг
		assert.Equal(t, backoffDuration, elapsedVirtualTime)
	})
}

func TestAccrualWorker_HandleBackoff_CancelInterrupt(t *testing.T) {
	logger := uzerolog.NewZerologAdapter(zerolog.New(nil)) // Выключаем логирование, чтобы не мусорить в аллокации
	worker := NewAccrualWorker(nil, nil, "", logger)

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		backoffDuration := 60 * time.Second
		startVirtualTime := time.Now()

		// Запускаем горутину, которая должна спать целую минуту (Backoff при 429)
		done := make(chan struct{})
		go func() {
			worker.handleBackoff(ctx, backoffDuration)
			close(done)
		}()

		// Даем горутине запуститься и встать в select/ожидание
		synctest.Wait()

		cancel()

		// Ждем, пока горутина обработает отмену контекста и выйдет из handleBackoff
		<-done

		endVirtualTime := time.Now()
		elapsedVirtualTime := endVirtualTime.Sub(startVirtualTime)

		assert.True(t, elapsedVirtualTime < backoffDuration, "Воркер должен был проснуться раньше при отмене ctx")
	})
}
