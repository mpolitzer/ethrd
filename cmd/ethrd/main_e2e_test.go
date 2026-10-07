//go:build e2e

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/mpolitzer/ethrd/db"
	"github.com/mpolitzer/ethrd/fixture"

	"github.com/ethereum/go-ethereum/accounts/abi/bind/v2"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/ethconfig"
	"github.com/ethereum/go-ethereum/ethclient/simulated"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	_ "modernc.org/sqlite"
)

// simulatedBlockchain is a simulated chain serving a real WS endpoint.
type simulatedBlockchain struct {
	sim      *simulated.Backend
	auth     *bind.TransactOpts
	contract *fixture.Fixture
	instance *bind.BoundContract
	port     int
}

func newSimulatedBlockchain(t *testing.T) *simulatedBlockchain {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := l.Addr().(*net.TCPAddr).Port
	require.NoError(t, l.Close()) // there is a TOCTOU on port

	key, err := crypto.GenerateKey()
	require.NoError(t, err)
	auth := bind.NewKeyedTransactor(key, params.AllDevChainProtocolChanges.ChainID)

	sim := simulated.NewBackend(map[common.Address]types.Account{
		auth.From: {Balance: big.NewInt(9e18)},
	}, func(nc *node.Config, _ *ethconfig.Config) {
		nc.WSHost = "127.0.0.1"
		nc.WSPort = port
		nc.WSModules = []string{"net", "web3", "eth"}
		nc.WSOrigins = []string{"*"}
	})
	t.Cleanup(func() { _ = sim.Close() })

	client := sim.Client()

	res, err := bind.LinkAndDeploy(
		&bind.DeploymentParams{Contracts: []*bind.MetaData{&fixture.FixtureMetaData}},
		bind.DefaultDeployer(auth, client),
	)
	require.NoError(t, err)
	sim.Commit()

	addr, err := bind.WaitDeployed(context.Background(), client, res.Txs[fixture.FixtureMetaData.ID].Hash())
	require.NoError(t, err)

	contract := fixture.NewFixture()
	return &simulatedBlockchain{
		sim:      sim,
		auth:     auth,
		contract: contract,
		instance: contract.Instance(client, addr),
		port:     port,
	}
}

// set sends fixture.set(id) and mines it into a new block.
func (s *simulatedBlockchain) set(t *testing.T, id int64) {
	t.Helper()
	_, err := bind.Transact(s.instance, s.auth, s.contract.PackSet(big.NewInt(id)))
	require.NoError(t, err)
	s.sim.Commit()
}

// mineTo commits empty blocks until the head reaches n.
func (s *simulatedBlockchain) mineTo(t *testing.T, n uint64) {
	t.Helper()
	for {
		h, err := s.sim.Client().HeaderByNumber(context.Background(), big.NewInt(int64(rpc.LatestBlockNumber)))
		require.NoError(t, err)
		if h.Number.Uint64() >= n {
			return
		}
		s.sim.Commit()
	}
}

// writeConf writes a fast-polling conf.yml: ws and db as redacted literals,
// combined-abi as a redacted file reference.
func writeConf(t *testing.T, dir, wsURL, dbValue string) string {
	t.Helper()
	abi, err := os.ReadFile(filepath.Join("..", "..", "combined-abi.json"))
	require.NoError(t, err)
	abiPath := filepath.Join(dir, "combined-abi.json")
	require.NoError(t, os.WriteFile(abiPath, abi, 0o644))

	confPath := filepath.Join(dir, "conf.yml")
	conf := fmt.Sprintf(`ws: %q
db: %q
ethrd:
  poll-interval: 50ms
  log-buf-size: 16
  filter:
    chunk-size: 8
    burst-limit: 100
    rate-limit: 1000
  bootstrap:
    block-number: 0
    combined-abi:
      file: %q
`, wsURL, dbValue, abiPath)
	require.NoError(t, os.WriteFile(confPath, []byte(conf), 0o644))
	return confPath
}

// waitFor polls cond every 10ms until it is true or d elapses.
func waitFor(t *testing.T, d time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	require.True(t, cond(), "condition not met within %s", d)
}

// TestRun drives the full pipeline in-process: conf -> db -> ws -> scan +
// subscription. It asserts the same end state the binary test used to: one
// finalized log, cursor past the mined range.
func TestRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	confirmTags := db.TagLatest | db.TagSafe | db.TagFinalized

	env := newSimulatedBlockchain(t)
	dir := t.TempDir()
	dbDSN := "file:" + filepath.Join(dir, "log.db") + "?_journal_mode=WAL&_busy_timeout=5000"
	confPath := writeConf(t, dir, fmt.Sprintf("ws://127.0.0.1:%d", env.port), dbDSN)

	done := make(chan error, 1)
	go func() { done <- run(ctx, confPath) }()

	env.set(t, 1) // block 2, log A
	env.mineTo(t, 32)

	// Assert on the db file through a second connection.
	check, err := sql.Open("sqlite", dbDSN)
	require.NoError(t, err)
	t.Cleanup(func() { _ = check.Close() })
	q := db.New(check)

	waitFor(t, 30*time.Second, func() bool {
		rows, err := q.SelectLogs(ctx, db.SelectLogsParams{Lo: 0, Hi: 1000})
		if err != nil || len(rows) != 1 {
			return false
		}
		cursor, err := q.GetCursor(ctx)
		return err == nil && cursor == 33 && rows[0].Tags == confirmTags
	})

	cancel()
	select {
	case err := <-done:
		// A cancel that lands mid-scan surfaces as context.Canceled.
		assert.True(t, err == nil || errors.Is(err, context.Canceled), "run: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("run did not return within 10s of cancel")
	}
}

func TestRunErrors(t *testing.T) {
	t.Run("missing conf", func(t *testing.T) {
		err := run(context.Background(), filepath.Join(t.TempDir(), "nope.yml"))
		assert.Error(t, err)
	})

	t.Run("unreachable ws", func(t *testing.T) {
		dir := t.TempDir()
		confPath := writeConf(t, dir, "ws://127.0.0.1:1", filepath.Join(dir, "log.db"))
		err := run(context.Background(), confPath)
		assert.ErrorContains(t, err, "connection refused")
	})
}
