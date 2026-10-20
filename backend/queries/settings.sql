-- Настройки центра (S8). finik_creds здесь не читаются и не пишутся.

-- name: GetCenterForUpdate :one
SELECT * FROM centers WHERE id = @id FOR UPDATE;

-- name: UpdateCenterSettings :exec
UPDATE centers
SET name = @name, phone = @phone, timezone = @timezone, billing_day = @billing_day,
    billing_day_next = @billing_day_next, billing_day_next_from = @billing_day_next_from,
    due_days = @due_days, excused_deducts = @excused_deducts, reminders_on = @reminders_on,
    pay_details = @pay_details
WHERE id = @id;
