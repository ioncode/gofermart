package router

import "net/http"

// Authenticator управляет жизненным циклом сессий и безопасностью периметра.
type Authenticator interface {
	Register(w http.ResponseWriter, r *http.Request)
	Login(w http.ResponseWriter, r *http.Request)
	AuthMiddleware(next http.Handler) http.Handler
}

// OrderManager отвечает за все операции, связанные с номерами заказов пользователей.
type OrderManager interface {
	UploadOrder(w http.ResponseWriter, r *http.Request)
	GetOrders(w http.ResponseWriter, r *http.Request)
}

// FinanceManager контролирует просмотр баланса и списание баллов лояльности.
type FinanceManager interface {
	GetBalance(w http.ResponseWriter, r *http.Request)
	Withdraw(w http.ResponseWriter, r *http.Request)
	GetWithdrawals(w http.ResponseWriter, r *http.Request)
}

// ServerHandler объединяет все три контекста с помощью композиции интерфейсов.
//go:generate mockery
//mockery:geterate: true
type ServerHandler interface {
	Authenticator
	OrderManager
	FinanceManager
}
