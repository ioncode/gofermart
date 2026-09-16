-- Добавляем баланс и заблокированные к списанию средства к пользователю (если еще не добавлены)
ALTER TABLE users ADD COLUMN IF NOT EXISTS balance NUMERIC(12, 2) DEFAULT 0.00;
ALTER TABLE users ADD COLUMN IF NOT EXISTS withdrawn NUMERIC(12, 2) DEFAULT 0.00;

-- Создаем перечисление (ENUM) для статусов заказа
DO $$ BEGIN
    CREATE TYPE order_status AS ENUM ('NEW', 'PROCESSING', 'INVALID', 'PROCESSED');
EXCEPTION
    WHEN duplicate_object THEN NULL;
END $$;

-- Таблица заказов
CREATE TABLE IF NOT EXISTS orders (
    id VARCHAR(64) PRIMARY KEY,              -- Номер заказа (последовательность цифр)
    user_id UUID NOT NULL,                  -- ID пользователя, загрузившего заказ
    status order_status NOT NULL DEFAULT 'NEW',
    accrual NUMERIC(12, 2) DEFAULT 0.00,    -- Начисленные баллы за этот заказ
    uploaded_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    
    FOREIGN KEY (user_id) REFERENCES users(id) ON DELETE CASCADE
);

-- Индекс для быстрой выборки списка заказов конкретного пользователя по дате
CREATE INDEX IF NOT EXISTS idx_orders_user_id_uploaded_at ON orders(user_id, uploaded_at DESC);
