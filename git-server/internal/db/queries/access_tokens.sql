-- name: CreateAccessToken :one
INSERT INTO access_tokens (user_id, token_hash, expires_at)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetAccessTokenByHash :one
SELECT * FROM access_tokens
WHERE token_hash = $1;

-- name: ListAccessTokensForUser :many
SELECT * FROM access_tokens
WHERE user_id = $1
ORDER BY created_at;

-- name: DeleteAccessToken :exec
DELETE FROM access_tokens
WHERE id = $1;
