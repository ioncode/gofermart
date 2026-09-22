-- 1. Удаляем индекс для сортировки заказов по дате
DROP INDEX IF EXISTS idx_orders_user_id_uploaded_at;

-- 2. Удаляем таблицу заказов (foreign key связи удалятся автоматически)
DROP TABLE IF EXISTS orders;

-- 3. Удаляем кастомный тип ENUM для статусов заказов
DROP TYPE IF EXISTS order_status;

-- 4. Откатываем изменения в таблице пользователей (удаляем колонки баланса)
ALTER TABLE users DROP COLUMN IF EXISTS balance;
ALTER TABLE users DROP COLUMN IF EXISTS withdrawn;
