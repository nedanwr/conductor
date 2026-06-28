-- name: CreatePlacement :one
INSERT INTO repo_placement (repo_id, storage_node_id)
VALUES ($1, $2)
RETURNING *;

-- name: ResolvePlacement :one
SELECT * FROM repo_placement
WHERE repo_id = $1;

-- name: DeletePlacement :exec
DELETE FROM repo_placement
WHERE repo_id = $1;
