package accrual

import (
	"context"
	"sync"
	"time"

	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/gofermart/internal/repository"
	"github.com/ioncode/ulog/v3"
	"golang.org/x/time/rate"
)

const (
	DefaultRequestsPerSecond = 5  // Стабильный превентивный темп: 5 запросов в секунду
	DefaultBurst             = 10 // Емкость бакета для сглаживания пиковых всплесков
)

type AccrualWorker struct {
	OrderChan chan domain.Order // Использует доменную модель из order.go
	service   *Service
	throttler *Throttler
	limiter   *rate.Limiter
	logger    ulog.Logger
}

func NewAccrualWorker(
	orderRepo repository.OrderRepository,
	accrualRepo repository.OrderAccrualRepository,
	accrualURL string,
	logger ulog.Logger,
) *AccrualWorker {
	throttler := &Throttler{}
	client := NewClient(accrualURL, 5*time.Second)

	return &AccrualWorker{
		OrderChan: make(chan domain.Order, 1000), // Буфер для предотвращения блокировок хендлеров
		throttler: throttler,
		limiter:   rate.NewLimiter(rate.Limit(DefaultRequestsPerSecond), DefaultBurst),
		logger:    logger.With(ulog.String("component", "accrual_worker")),
		service: &Service{
			orderRepo:   orderRepo,
			accrualRepo: accrualRepo,
			client:      client,
			throttler:   throttler,
			logger:      logger,
		},
	}
}

// Start запускает диспетчер обработки входящего канала и тикера подстраховки.
func (w *AccrualWorker) Start(ctx context.Context, interval time.Duration) error {
	w.logger.Info("Фоновый диспетчер воркеров запущен")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Оставляем только sync.WaitGroup для корректного Graceful Shutdown
	var wg sync.WaitGroup

	for {
		// ДИНАМИЧЕСКИЙ КАНАЛ: Если реактивный троттлер активен (429), activeChan становится nil.
		// Чтение из nil-канала блокируется, поэтому select начнет игнорировать этот кейс.
		// Заказы не вычитываются и безопасно ждут в буфере OrderChan, а CPU не тратится на холостые циклы.
		var activeChan chan domain.Order
		if !w.throttler.IsBlocked() {
			activeChan = w.OrderChan
		}

		select {
		case <-ctx.Done():
			w.logger.Info("Получен сигнал завершения приложения. Ожидаем плавного закрытия активных задач...")
			wg.Wait() // Удерживает горутину в main.go, пока текущие запущенные воркеры дорабатывают
			return ctx.Err()

		case <-ticker.C:
			w.processUnprocessedOrders(ctx)

		// Слушаем динамический канал вместо оригинального w.OrderChan напрямую
		case order, ok := <-activeChan:
			if !ok {
				wg.Wait()
				return nil
			}

			// ПРЕВЕНТИВНАЯ ЗАЩИТА: Ограничиваем общую частоту отправки до 5 запросов/сек.
			// Так как семафор удален, лимитер выполняет всю работу по сглаживанию нагрузки.
			if err := w.limiter.Wait(ctx); err != nil {
				continue
			}

			ord := order
			wg.Add(1)

			// Запускаем сетевой запрос асинхронно. Количество одновременно работающих
			// горутин теперь естественным образом ограничено скоростью лимитера.
			go func() {
				defer wg.Done()
				w.service.ProcessOrder(ctx, ord, "channel")
			}()
		}
	}
}

// processUnprocessedOrders периодически выгребает «застрявшие» заказы через слой бизнес-логики.
func (w *AccrualWorker) processUnprocessedOrders(ctx context.Context) {
	// Если система находится в режиме экстренной паузы после HTTP 429 —
	// даже не идем в базу данных, бережем ресурсы бэкенда.
	if w.throttler.IsBlocked() {
		w.logger.Debug("Обнаружена экстренная пауза 429 при старте тикера. Прерываем итерацию.")
		return
	}

	orders, err := w.service.GetUnprocessedOrders(ctx)
	if err != nil {
		w.logger.Error("Не удалось получить необработраные заказы из сервиса", err)
		return
	}

	for _, order := range orders {
		if ctx.Err() != nil {
			return
		}

		// Если в процессе последовательного обхода массива одна из соседних фоновых горутин
		// или этот же цикл поймали 429 — мгновенно прекращаем выполнение.
		if w.throttler.IsBlocked() {
			w.logger.Debug("Обнаружена экстренная пауза 429 во время обхода тикера. Прерываем итерацию.")
			return
		}

		// Поток тикера также обязан уважать общий превентивный лимит в 5 запросов/сек.
		if err := w.limiter.Wait(ctx); err != nil {
			return
		}

		w.service.ProcessOrder(ctx, order, "ticker")
	}
}
