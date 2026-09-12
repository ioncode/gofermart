package handler

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/ioncode/gofermart/internal/service"
)

// Login обрабатывает запрос POST /api/user/login
func (h *UserHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req RegisterRequest

	// Читаем и парсим JSON через наш пуленный хелпер со скоростью ~85 ns.
	// Если формат неверный (400) или тело > 4 КБ (413) — хелпер сам ответит и вернет false.
	if !ReadJSONOptimized(w, r, &req) {
		return
	}

	// Валидация данных: очищаем пробелы с обеих сторон строк
	req.Login = strings.TrimSpace(req.Login)
	req.Password = strings.TrimSpace(req.Password)

	if req.Login == "" || req.Password == "" {
		http.Error(w, "Login and password are required", http.StatusBadRequest) // 400
		return
	}

	// 4. Передаем строго типизированные string аргументы в слой сервиса
	token, err := h.service.Authenticate(r.Context(), req.Login, req.Password)
	if err != nil {
		if errors.Is(err, service.ErrInvalidCredentials) {
			// 401 — неверная пара логин/пароль
			http.Error(w, "Invalid login or password", http.StatusUnauthorized)
			return
		}
		// 500 — внутренняя ошибка сервера (база данных недоступна, сбой сети и т.д.)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	// 5. Установка JWT-токена в cookies (в соответствии с ТЗ)
	http.SetCookie(w, &http.Cookie{
		Name:     "auth_token",
		Value:    token,
		Path:     "/",
		Expires:  time.Now().Add(24 * time.Hour), // Время жизни куки совпадает с TTL токена
		HttpOnly: true,                           // Защита от кражи токена скриптами через XSS-атаки
		Secure:   h.isProd,                       // Включаем только для HTTPS сред
		SameSite: http.SameSiteLaxMode,           // Базовая защита от CSRF-атак
	})

	// 200 — пользователь успешно аутентифицирован
	w.WriteHeader(http.StatusOK)
}
