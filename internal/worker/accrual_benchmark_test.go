package worker

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ioncode/gofermart/internal/domain"
	"github.com/ioncode/ulog/v3/adapters/uzerolog"
	"github.com/rs/zerolog"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"
)

// Пассивные стабы репозиториев для исключения влияния дискового I/O на замеры CPU воркера
type stubOrderRepo struct{}

func (s *stubOrderRepo) GetUnprocessedOrders(ctx context.Context) ([]domain.Order, error) {
	return nil, nil
}
func (s *stubOrderRepo) GetOrder(ctx context.Context, id string) (domain.Order, error) {
	return domain.Order{}, nil
}

func (s *stubOrderRepo) CreateOrder(context.Context, string, string, string) error {
	return nil
}

func (s *stubOrderRepo) GetOrdersByUserID(context.Context, string) ([]domain.Order, error) {
	return nil, nil
}

type stubOrderAccrualRepo struct{}

func (s *stubOrderAccrualRepo) UpdateOrderAndBalance(ctx context.Context, orderID, userID, status string, accrual decimal.Decimal) error {
	return nil
}

func BenchmarkAccrualWorker_ChannelProcessing(b *testing.B) {
	logger := uzerolog.NewZerologAdapter(zerolog.New(nil)) // Выключаем логирование, чтобы не мусорить в аллокации

	// Создаем фейковый HTTP-сервер, имитирующий моментальный ответ системы accrual (200 OK)
	serverAccrual := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"order":"123","status":"PROCESSED","accrual":10.5}`))
	}))
	defer serverAccrual.Close()

	orderRepo := &stubOrderRepo{}
	accrualRepo := &stubOrderAccrualRepo{}

	worker := NewAccrualWorker(orderRepo, accrualRepo, serverAccrual.URL, logger)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Запускаем воркер с errgroup.SetLimit под капотом асинхронно
	var eg errgroup.Group
	eg.Go(func() error {
		// Передаем большой интервал тикера, чтобы в рамках бенчмарка тестировать ТОЛЬКО чтение из канала
		return worker.Start(ctx, 1*time.Hour)
	})

	testOrder := domain.Order{
		ID:     "12345678903",
		UserID: "user-1",
		Status: domain.StatusNew,
	}

	// Сбрасываем таймер перед входом в цикл замеров
	b.ResetTimer()

	for b.Loop() {
		worker.OrderChan <- testOrder
	}

	// Остановка замеров времени перед деструктуризацией ресурсов
	b.StopTimer()

	// Закрываем канал и ждем завершения горутин группы
	close(worker.OrderChan)
	_ = eg.Wait()
}
