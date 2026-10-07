-- +goose up

-- types.Log + extra control fields
CREATE TABLE log (
	-- contents of types.Log from go-ethereum
	tx_hash         BYTES(32) NOT NULL,
	tx_index        UINT64    NOT NULL, -- global per block
	log_index       UINT64    NOT NULL, -- position in block (ref. go-ethereum AddLog)
	block_number    UINT64    NOT NULL,
	block_hash      BYTES(32) NOT NULL,
	block_timestamp UINT64    NOT NULL,
	address         BYTES(20) NOT NULL,
	topics          BLOB      NOT NULL,
	data            BLOB      NOT NULL,

	tags            UINT64    NOT NULL DEFAULT 0, -- bit0=latest, bit1=safe, bit2=finalized, bit3=removed

	CHECK((tags & 4) = 0 OR (tags & 8) = 0),      -- `finalized` and `removed` are mutually exclusive
	PRIMARY KEY (block_hash, log_index)
);

-- merge key order described in: doc/ethrd.md
CREATE INDEX log_block_number_log_index_block_hash ON log (block_number, log_index, block_hash);

CREATE TABLE state (
	id                 INTEGER PRIMARY KEY CHECK (id = 1), -- ID for singleton
	finalized_frontier UINT64  NOT NULL DEFAULT 0,
	combined_abi       TEXT    NOT NULL  -- the set of logs and their descriptions
);

-- +goose down
DROP INDEX IF EXISTS log_block_number_log_index_block_hash;
DROP TABLE IF EXISTS log;
DROP TABLE IF EXISTS state;
