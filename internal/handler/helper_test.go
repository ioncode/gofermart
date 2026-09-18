package handler

import (
	"bytes"

	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "github.com/goccy/go-json"
	"github.com/stretchr/testify/assert"
)

type testTarget struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

func TestReadJSONOptimized(t *testing.T) {
	t.Run("Success valid JSON", func(t *testing.T) {
		jsonBody := `{"login":"test_user","password":"secure_password"}`
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(jsonBody))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		var dst testTarget
		ok := ReadJSONOptimized(rec, req, &dst)

		assert.True(t, ok)
		assert.Equal(t, "test_user", dst.Login)
		assert.Equal(t, "secure_password", dst.Password)
	})

	t.Run("Failure empty body", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(""))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		var dst testTarget
		ok := ReadJSONOptimized(rec, req, &dst)

		assert.False(t, ok)
		assert.Equal(t, http.StatusBadRequest, rec.Code) // 400
	})

	t.Run("Failure malformed JSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"login": "user"`)) // Брак в синтаксисе
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		var dst testTarget
		ok := ReadJSONOptimized(rec, req, &dst)

		assert.False(t, ok)
		assert.Equal(t, http.StatusBadRequest, rec.Code) // 400
	})

	t.Run("Failure too large body by MaxBytesReader", func(t *testing.T) {
		// Создаем тело запроса, которое заведомо больше нашего лимита
		// (например, 20 байт при лимите в 10 байт)
		largeBody := []byte(`{"login":"very_long_username_that_exceeds_limit"}`)

		req := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(largeBody))
		rec := httptest.NewRecorder()

		// Имитируем поведение Middleware: оборачиваем поток в MaxBytesReader с маленьким лимитом
		req.Body = http.MaxBytesReader(rec, req.Body, 10)

		var dst testTarget
		// Вызываем наш оптимизированный хелпер
		ok := ReadJSONOptimized(rec, req, &dst)

		// Проверяем, что функция вернула false (запрос отклонен)
		assert.False(t, ok)

		// Проверяем, что клиенту ушел правильный HTTP-статус 413 Payload Too Large
		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)

		// Проверяем, что буфер структуры остался пустым и данные не протекли
		assert.Empty(t, dst.Login)
	})
}

// Стандартный подход, используемый в большинстве приложений на Go
func ReadJSONStandard(w http.ResponseWriter, r *http.Request, dst any) bool {
	ct := r.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		http.Error(w, "Unsupported Media Type", http.StatusUnsupportedMediaType)
		return false
	}
	defer r.Body.Close()

	// io.ReadAll выделяет новую память в куче под каждый запрос
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return false
	}

	// Стандартный encoding/json работает через рефлексию и сильно нагружает GC
	if err := json.Unmarshal(bodyBytes, dst); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return false
	}

	return true
}

// Бенчмарк для стандартного подхода
func BenchmarkReadJSONStandard(b *testing.B) {
	jsonBody := `{"login":"benchmark_user_name_test","password":"super_secure_password_string_123"}`
	rec := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Каждый шаг симулирует новый входящий HTTP-запрос
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		var dst testTarget
		_ = ReadJSONStandard(rec, req, &dst)
	}
}

// Бенчмарк оптимизированного подхода
func BenchmarkReadJSONOptimized(b *testing.B) {
	jsonBody := `{"login":"benchmark_user_name_test","password":"super_secure_password_string_123"}`
	rec := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(jsonBody))
		req.Header.Set("Content-Type", "application/json")

		var dst testTarget
		_ = ReadJSONOptimized(rec, req, &dst)
	}
}

// Тестовая структура для честного замера парсинга
type benchmarkPayload struct {
	buf  []byte
	body *bytes.Reader
}

func prepareBenchmarkData(jsonStr string) *benchmarkPayload {
	b := []byte(jsonStr)
	return &benchmarkPayload{
		buf:  b,
		body: bytes.NewReader(b),
	}
}

// Честный бенчмарк для стандартного подхода
func BenchmarkPureStandard(b *testing.B) {
	jsonBody := `{"login":"benchmark_user_name_test","password":"super_secure_password_string_123"}`
	data := prepareBenchmarkData(jsonBody)
	rec := httptest.NewRecorder()

	// Создаем один запрос на все время теста
	req := httptest.NewRequest(http.MethodPost, "/", data.body)
	req.Header.Set("Content-Type", "application/json")

	var dst testTarget

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Сбрасываем поток чтения в начало перед каждым вызовом
		_, _ = data.body.Seek(0, io.SeekStart)

		_ = ReadJSONStandard(rec, req, &dst)
	}
}

// Честный бенчмарк для оптимизированного подхода
func BenchmarkPureOptimized(b *testing.B) {
	jsonBody := `{"login":"benchmark_user_name_test","password":"super_secure_password_string_123"}`
	data := prepareBenchmarkData(jsonBody)
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/", data.body)
	req.Header.Set("Content-Type", "application/json")

	// Выделяем память под структуру один раз до сброса таймера,
	// чтобы тест замерял исключительно перформанс самого хелпера.
	dst := new(testTarget)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = data.body.Seek(0, io.SeekStart)

		_ = ReadJSONOptimized(rec, req, dst)
	}
}

// Вариант со стриминговым декодером goccy/go-json
func ReadJSONDecoderWithGoccy(w http.ResponseWriter, r *http.Request, dst any) bool {
	ct := r.Header.Get("Content-Type")
	if len(ct) < 16 || ct[:16] != "application/json" {
		http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
		return false
	}
	defer r.Body.Close()

	// Читаем напрямую из потока r.Body без io.ReadAll
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		http.Error(w, "Bad Request", http.StatusBadRequest)
		return false
	}

	return true
}

// Бенчмарк для стримингового декодера
func BenchmarkPureDecoder(b *testing.B) {
	jsonBody := `{"login":"benchmark_user_name_test","password":"super_secure_password_string_123"}`
	data := prepareBenchmarkData(jsonBody)
	rec := httptest.NewRecorder()

	req := httptest.NewRequest(http.MethodPost, "/", data.body)
	req.Header.Set("Content-Type", "application/json")

	var dst testTarget

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = data.body.Seek(0, io.SeekStart)

		_ = ReadJSONDecoderWithGoccy(rec, req, &dst)
	}
}

func BenchmarkStage1_ReadBody(b *testing.B) {
	jsonBody := `{"login":"benchmark_user_name_test","password":"super_secure_password_string_123"}`
	data := prepareBenchmarkData(jsonBody)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", data.body)
	req.Header.Set("Content-Type", "application/json")

	// Пустой коллбэк, который ничего не делает с байтами,
	// чтобы замерить чистую скорость сетевого слоя.
	noopProcessor := func(payload []byte) bool {
		return true
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = data.body.Seek(0, io.SeekStart)

		// Вызываем чтение с коллбэком
		_ = ReadBodyOptimized(rec, req, noopProcessor)
	}
}

// 2. Проверяем парсинг JSON из уже готового буфера
func BenchmarkStage2_Unmarshal(b *testing.B) {
	payload := []byte(`{"login":"benchmark_user_name_test","password":"super_secure_password_string_123"}`)
	var dst testTarget

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Замеряем только то, что делает внешняя библиотека
		_ = json.Unmarshal(payload, &dst)
	}
}
