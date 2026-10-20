-- name: InsertAuditLog :exec
INSERT INTO audit_log (center_id, user_id, action, entity, entity_id, before, after)
VALUES (@center_id, @user_id, @action, @entity, @entity_id, @before, @after);
