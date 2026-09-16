DROP TABLE IF EXISTS withdrawals;

ALTER TABLE users DROP CONSTRAINT IF EXISTS check_balance_non_negative;
