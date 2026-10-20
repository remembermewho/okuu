-- ledger: операции не удаляются и не редактируются (R15). Знак: charge/refund < 0, payment > 0.

-- name: InsertCharge :one
INSERT INTO ledger (center_id, student_id, enrollment_id, kind, amount, period, due_date,
                    list_price, discount, comment, created_by)
VALUES (@center_id, @student_id, @enrollment_id, 'charge', @amount, @period, @due_date,
        @list_price, @discount, @comment, @created_by)
RETURNING *;

-- name: StudentBalance :one
SELECT COALESCE(sum(amount), 0)::bigint
FROM ledger
WHERE center_id = @center_id AND student_id = @student_id AND cancelled_at IS NULL;
