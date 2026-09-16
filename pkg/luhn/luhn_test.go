package luhn

import "testing"

func TestIsValid(t *testing.T) {
	// Определяем тестовые сценарии (валидные номера, невалидные, пограничные случаи)
	tests := []struct {
		name   string
		number string
		want   bool
	}{
		{
			name:   "Valid order number (Standard example)",
			number: "12345678903",
			want:   true,
		},
		{
			name:   "Valid order number (Single digit zero)",
			number: "0",
			want:   true,
		},
		{
			name:   "Valid order number (Another long valid case)",
			number: "79927398713",
			want:   true,
		},
		{
			name:   "Invalid order number (Wrong check digit)",
			number: "12345678904",
			want:   false,
		},
		{
			name:   "Invalid order number (All zeros except last)",
			number: "00000000001",
			want:   false,
		},
		{
			name:   "Empty string",
			number: "",
			want:   false,
		},
		{
			name:   "String with non-digits (Letters)",
			number: "12345a78903",
			want:   false,
		},
		{
			name:   "String with non-digits (Spaces)",
			number: "1234 5678 903",
			want:   false,
		},
		{
			name:   "String with special characters",
			number: "1234-5678-903",
			want:   false,
		},
	}

	for _, tt := range tests {
		// Запускаем каждый тест как изолированный субтест (subtest)
		t.Run(tt.name, func(t *testing.T) {
			if got := IsValid(tt.number); got != tt.want {
				t.Errorf("IsValid() = %v, want %v for number %q", got, tt.want, tt.number)
			}
		})
	}
}

// Тест производительности (Benchmark) для проверки скорости работы алгоритма
func BenchmarkIsValid(b *testing.B) {
	number := "12345678903"
	for i := 0; i < b.N; i++ {
		_ = IsValid(number)
	}
}
