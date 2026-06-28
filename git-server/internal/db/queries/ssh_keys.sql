-- name: CreateSSHKey :one
INSERT INTO ssh_keys (user_id, public_key, fingerprint)
VALUES ($1, $2, $3)
RETURNING *;

-- name: GetSSHKeyByFingerprint :one
SELECT * FROM ssh_keys
WHERE fingerprint = $1;

-- name: ListSSHKeysForUser :many
SELECT * FROM ssh_keys
WHERE user_id = $1
ORDER BY created_at;

-- name: DeleteSSHKey :exec
DELETE FROM ssh_keys
WHERE id = $1;
