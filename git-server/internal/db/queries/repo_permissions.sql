-- name: GrantPermission :one
INSERT INTO repo_permissions (user_id, repo_id, level)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, repo_id) DO UPDATE SET level = EXCLUDED.level
RETURNING *;

-- name: GetPermission :one
SELECT * FROM repo_permissions
WHERE user_id = $1 AND repo_id = $2;

-- name: ListPermissionsForRepo :many
SELECT * FROM repo_permissions
WHERE repo_id = $1;

-- name: RevokePermission :exec
DELETE FROM repo_permissions
WHERE user_id = $1 AND repo_id = $2;
