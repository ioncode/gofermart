package luhn

// IsValid проверяет строку цифр на корректность по алгоритму Луна
func IsValid(number string) bool {
	if len(number) == 0 {
		return false
	}

	var sum int
	parity := len(number) % 2

	for i, r := range number {
		if r < '0' || r > '9' {
			return false // Номер должен состоять только из цифр
		}

		digit := int(r - '0')
		if i%2 == parity {
			digit *= 2
			if digit > 9 {
				digit -= 9
			}
		}
		sum += digit
	}

	return sum%10 == 0
}
