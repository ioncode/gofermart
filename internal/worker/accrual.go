// Пакет worker инкапсулирует логику фоновых процессов приложения,
// в частности, взаимодействие с внешней системой расчета баллов лояльности.
package worker

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"sync"
	"time"

	json "github.com/goccy/go-json"
	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/gofermart/internal/repository"
	"github.com/ioncode/ulog/v3"
	"github.com/shopspring/decimal"
)

// AccrualResponse описывает структуру JSON-ответа, получаемого
// от внешнего сервиса расчета баллов начислений.
type AccrualResponse struct {
	// Order представляет собой уникальный номер заказа в виде строки.
	Order string `json:"order"`
	// Status содержит текущий статус обработки заказа во внешней системе.
	Status domain.AccrualStatus `json:"status"`
	// Accrual хранит количество начисленных баллов (может быть nil для промежуточных статусов).
	Accrual *decimal.Decimal `json:"accrual,omitempty"`
}

// AccrualWorker представляет собой фоновый процессор, который асинхронно
// опрашивает внешнюю систему начислений и обновляет балансы пользователей.
type AccrualWorker struct {
	orderRepo   repository.OrderRepository
	accrualRepo repository.OrderAccrualRepository
	accrualURL  string
	client      *http.Client
	logger      ulog.Logger
	// OrderChan служит для мгновенного получения новых заказов из слоя бизнес-логики.
	OrderChan chan domain.Order
	// processing используется для предотвращения одновременной обработки одного заказа тикером и каналом.
	processing sync.Map
}

// NewAccrualWorker инициализирует и возвращает новый экземпляр AccrualWorker.
// Настраивает оптимизированный HTTP-транспорт для минимизации переоткрытия TCP-соединений.
func NewAccrualWorker(
	orderRepo repository.OrderRepository,
	accrualRepo repository.OrderAccrualRepository,
	accrualURL string,
	logger ulog.Logger,
) *AccrualWorker {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.MaxIdleConns = 100
	transport.MaxIdleConnsPerHost = 100
	transport.IdleConnTimeout = 90 * time.Second

	return &AccrualWorker{
		orderRepo:   orderRepo,
		accrualRepo: accrualRepo,
		accrualURL:  accrualURL,
		client: &http.Client{
			Timeout:   5 * time.Second,
			Transport: transport,
		},
		logger:    logger.With(ulog.String("component", "accrual_worker")),
		OrderChan: make(chan domain.Order, 1000), // Буферизация для исключения блокировок хендлеров
	}
}

// Start запускает бесконечный цикл обработки заказов. Метод слушает сигналы отмены контекста,
// входящие события из канала мгновенной обработки и периодические тики для подстраховки застрявших заказов.
// Должен запускаться в отдельной горутине: `go worker.Start(rootCtx, 5*time.Second)`.
func (w *AccrualWorker) Start(ctx context.Context, interval time.Duration) {
	w.logger.Info("Фоновый событийный воркер расчета баллов запущен")
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			w.logger.Info("Фоновый воркер останавливает работу (получен сигнал отмены контекста)")
			return

		case <-ticker.C:
			w.processUnprocessedOrders(ctx)

		case order, ok := <-w.OrderChan:
			if !ok {
				w.logger.Error("Канал заказов был закрыт", nil)
				return
			}
			w.processSingleOrder(ctx, order)
		}
	}
}

// processSingleOrder выполняет немедленную обработку одного заказа, пришедшего из Go-канала.
// Метод защищен внутренним механизмом recover() от аварийного завершения всего приложения.
func (w *AccrualWorker) processSingleOrder(ctx context.Context, order domain.Order) {
	defer func() {
		if r := recover(); r != nil {
			w.logger.With(ulog.String("order_id", order.ID)).Error(
				fmt.Sprintf("КРИТИЧЕСКАЯ ПАНИКА при мгновенной обработке из канала: %v", r),
				nil,
			)
			w.processing.Delete(order.ID)
		}
	}()

	if _, loaded := w.processing.LoadOrStore(order.ID, true); loaded {
		return
	}
	defer w.processing.Delete(order.ID)

	backoff, err := w.checkOrderAccrual(ctx, order)
	if err != nil {
		w.logger.With(ulog.String("order_id", order.ID)).Error("Ошибка мгновенной обработки заказа", err)
	}
	if backoff > 0 {
		w.handleBackoff(ctx, backoff)
	}
}

// processUnprocessedOrders вычитывает из базы данных список заказов со статусами NEW и PROCESSING
// и последовательно отправляет их на проверку. Паника на одном заказе изолируется и не прерывает цикл.
func (w *AccrualWorker) processUnprocessedOrders(ctx context.Context) {
	orders, err := w.orderRepo.GetUnprocessedOrders(ctx)
	if err != nil {
		w.logger.Error("Не удалось получить необработанные заказы из БД", err)
		return
	}

	for _, order := range orders {
		if ctx.Err() != nil {
			return
		}

		orderLogger := w.logger.With(ulog.String("order_id", order.ID))

		if _, loaded := w.processing.LoadOrStore(order.ID, true); loaded {
			continue
		}

		// Изолированная итерация цикла через анонимную функцию для локализации recover() и defer
		func() {
			defer func() {
				if r := recover(); r != nil {
					orderLogger.Error(fmt.Sprintf("КРИТИЧЕСКАЯ ПАНИКА при плановой обработке: %v", r), nil)
				}
				w.processing.Delete(order.ID)
			}()

			backoff, err := w.checkOrderAccrual(ctx, order)
			if err != nil {
				orderLogger.Error("Ошибка плановой обработки заказа", err)
			}

			if backoff > 0 {
				w.handleBackoff(ctx, backoff)
			}
		}()
	}
}

// handleBackoff переводит воркер в состояние ожидания при получении ответа HTTP 429 Too Many Requests.
// Сон является неблокирующим и может быть прерван отменой контекста приложения (Graceful Shutdown).
func (w *AccrualWorker) handleBackoff(ctx context.Context, backoff time.Duration) {
	w.logger.Debug(fmt.Sprintf("Получен Rate Limit от Accrual. Засыпаем на %v", backoff))
	select {
	case <-ctx.Done():
	case <-time.After(backoff):
	}
}

// checkOrderAccrual осуществляет сетевой запрос к внешней системе начислений,
// валидирует HTTP-статусы, парсит JSON-ответ и вызывает транзакционное обновление состояния в БД.
// Возвращает таймаут ожидания (backoff), если внешняя система запросила ограничение лимитов.
func (w *AccrualWorker) checkOrderAccrual(ctx context.Context, order domain.Order) (time.Duration, error) {
	// Создаем изолированный сублоггер для текущего заказа
	orderLogger := w.logger.With(ulog.String("order_id", order.ID))

	// Ограничиваем время выполнения конкретного HTTP-запроса (согласовано с таймаутом клиента в 5 секунд)
	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	url := fmt.Sprintf("%s/api/orders/%s", w.accrualURL, order.ID)
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("http request to accrual failed: %w", err)
	}
	defer resp.Body.Close()

	// ТЗ требует обязательной обработки статуса 429 Too Many Requests (Rate Limiting внешнего сервиса)
	if resp.StatusCode == http.StatusTooManyRequests {
		retryAfterHeader := resp.Header.Get("Retry-After")
		seconds, err := strconv.Atoi(retryAfterHeader)
		if err != nil {
			return 2 * time.Second, nil // Дефолтная пауза при отсутствии или повреждении заголовка
		}
		return time.Duration(seconds) * time.Second, nil
	}

	// Код 204 означает, что заказ еще не зарегистрирован в accrual. Это штатная ситуация.
	if resp.StatusCode == http.StatusNoContent {
		orderLogger.Debug("Заказ еще не зарегистрирован в системе accrual (204)")
		return 0, nil
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("unexpected status code from accrual: %d", resp.StatusCode)
	}

	// Оптимизация аллокаций: читаем через слайс байт, что быстрее для маленьких объектов
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response body: %w", err)
	}

	var accResp AccrualResponse
	if err := json.Unmarshal(bodyBytes, &accResp); err != nil {
		return 0, fmt.Errorf("failed to unmarshal json response: %w", err)
	}

	// Логика маршрутизации финальных статусов (PROCESSED, INVALID)
	if accResp.Status.IsFinal() {
		var val decimal.Decimal // По умолчанию равен точному нулю (0)
		if accResp.Accrual != nil {
			val = *accResp.Accrual
		}

		// Переводим заказ в финальный статус и начисляем баллы в одной ACID транзакции
		err = w.accrualRepo.UpdateOrderAndBalance(ctx, order.ID, order.UserID, string(accResp.Status), val)
		if err != nil {
			return 0, fmt.Errorf("failed to update final order state in db: %w", err)
		}
		orderLogger.Info(fmt.Sprintf("Заказ успешно закрыт со статусом: %s. Начислено баллов: %s", accResp.Status, val.String()))
		return 0, nil
	}

	// Логика фиксации промежуточного статуса (PROCESSING) для прозрачности данных
	if accResp.Status == domain.AccrualProcessing && order.Status == domain.StatusNew {
		_ = w.accrualRepo.UpdateOrderAndBalance(ctx, order.ID, order.UserID, string(domain.StatusProcessing), decimal.NewFromInt(0))
		orderLogger.Debug("Заказ переведен в промежуточный статус PROCESSING")
	}

	return 0, nil
}
