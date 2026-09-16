-- 1. Добавляем проверку, чтобы баланс пользователя никогда не становился отрицательным
ALTER TABLE users 
ADD CONSTRAINT check_balance_non_negative CHECK (balance >= 0.00);

-- 2. Создаем таблицу для учета истории вывода средств (списаний)
CREATE TABLE IF NOT EXISTS withdrawals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL,
    order_id VARCHAR(64) NOT NULL UNIQUE, -- Номер гипотетического заказа
    amount NUMERIC(12, 2) NOT NULL,       -- Соответствует точности NUMERIC(12,2) из миграции orders
    processed_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- 3. Индекс для быстрой выборки истории списаний конкретного пользователя от новых к старым
CREATE INDEX IF NOT EXISTS idx_withdrawals_user_id_processed_at ON withdrawals(user_id, processed_at DESC);
