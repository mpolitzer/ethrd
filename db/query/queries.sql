-- name: UpsertState :exec
INSERT INTO state (id, finalized_frontier, combined_abi)
VALUES (1, sqlc.arg(frontier), sqlc.arg(combined_abi))
ON CONFLICT (id) DO NOTHING;

-- name: GetCombinedABI :one
SELECT combined_abi FROM state WHERE id = 1;

-- name: GetCursor :one
SELECT finalized_frontier FROM state WHERE id = 1;

-- name: AdvanceFinalizedFrontier :exec
UPDATE state SET finalized_frontier = sqlc.arg(finalized_frontier)
WHERE id = 1 AND sqlc.arg(finalized_frontier) >= finalized_frontier;

-- name: ApplyLogEdit :execrows
INSERT INTO log (
	tx_hash,
	tx_index,
	log_index,
	block_number,
	block_hash,
	block_timestamp,
	address,
	topics,
	data,
	tags
) SELECT
	sqlc.arg(tx_hash),
	sqlc.arg(tx_index),
	sqlc.arg(log_index),
	sqlc.arg(block_number),
	sqlc.arg(block_hash),
	sqlc.arg(block_timestamp),
	sqlc.arg(address),
	sqlc.arg(topics),
	sqlc.arg(data),

	sqlc.arg(tags)
WHERE sqlc.arg(block_number) >= (SELECT finalized_frontier FROM state WHERE id = 1)
ON CONFLICT (block_hash, log_index) DO UPDATE SET tags = log.tags | excluded.tags;

-- name: SelectLogs :many
SELECT * FROM log
WHERE block_number BETWEEN sqlc.arg(lo) AND sqlc.arg(hi)
ORDER BY block_number, log_index, block_hash;

-- name: SelectLogsWindowAsc :many
SELECT * FROM log
WHERE (block_number,log_index,block_hash) > (sqlc.arg(block_number),sqlc.arg(log_index),sqlc.arg(block_hash))
ORDER BY block_number ASC, log_index ASC, block_hash ASC
LIMIT sqlc.arg(limit);

-- name: SelectLogsWindowDesc :many
SELECT * FROM log
WHERE (block_number,log_index,block_hash) < (sqlc.arg(block_number),sqlc.arg(log_index),sqlc.arg(block_hash))
ORDER BY block_number DESC, log_index DESC, block_hash DESC
LIMIT sqlc.arg(limit);
