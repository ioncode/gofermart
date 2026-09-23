package accrual

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
)

// TestThrottler_Block_VirtualTime проверяет, что блокировка действительно
// длится ровно заданное время, используя механизмы виртуального времени synctest.
func TestThrottler_Block_VirtualTime(t *testing.T) {
	throttler := &Throttler{}
	blockDuration := 60 * time.Second

	// Запускаем тест в изолированном "пузыре" виртуального времени synctest
	synctest.Test(t, func(t *testing.T) {
		startVirtualTime := time.Now()

		// 1. Активируем блокировку (под капотом она выставит atomic-переменную)
		throttler.Block(blockDuration)

		// Проверяем, что в текущую виртуальную миллисекунду мы заблокированы
		assert.True(t, throttler.IsBlocked())

		// 2. Имитируем ожидание. Нам нужно дождаться, пока виртуальное время
		// сдвинется вперед. Внутри synctest.Test функция time.Sleep сдвигает
		// виртуальные часы мгновенно, не тратя реальное время процессора.
		time.Sleep(blockDuration)

		endVirtualTime := time.Now()
		elapsedVirtualTime := endVirtualTime.Sub(startVirtualTime)

		// Проверяем, что виртуально прошло ровно 60 секунд
		assert.Equal(t, blockDuration, elapsedVirtualTime)

		// 3. Проверяем, что по истечении времени блокировка автоматически снялась
		assert.False(t, throttler.IsBlocked(), "Блокировка должна была закончиться")
	})
}

// TestThrottler_Block_RemainingTime проверяет корректность вычисления
// остаточного времени блокировки при сдвиге виртуального времени.
func TestThrottler_Block_RemainingTime(t *testing.T) {
	// Для этого теста мы временно добавим метод RemainingBlockTime в Throttler,
	// который мы обсуждали на этапе проектирования декомпозиции.
	throttler := &Throttler{}
	blockDuration := 10 * time.Second

	synctest.Test(t, func(t *testing.T) {
		throttler.Block(blockDuration)

		// Сдвигаем виртуальное время на 4 секунды вперед
		time.Sleep(4 * time.Second)

		// Вычисляем остаток. Из 10 секунд должно остаться около 6 секунд.
		// В виртуальном времени synctest расчет будет математически точным.
		until := throttler.blockedUntil.Load()
		var remaining time.Duration
		if until != nil {
			remaining = time.Until(*until)
		}

		// Допускаем погрешность в пределах виртуального кванта времени,
		// но обычно в synctest это ровно 6 секунд.
		assert.InDelta(t, 6*time.Second, remaining, float64(10*time.Millisecond))
	})
}
