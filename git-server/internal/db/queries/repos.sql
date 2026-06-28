-- name: CreateRepo :one
INSERT INTO repos (owner, name, visibility, default_branch)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: GetRepo :one
SELECT * FROM repos
WHERE id = $1;

-- name: GetRepoByOwnerName :one
SELECT * FROM repos
WHERE owner = $1 AND name = $2;

-- name: DeleteRepo :exec
DELETE FROM repos
WHERE id = $1;
