package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const confYAML = `ws: "ws://example"
db: "file:test.db?_journal_mode=WAL"
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
`

func TestConfLoad(t *testing.T) {
	dir := t.TempDir()
	abiPath := filepath.Join(dir, "combined-abi.json")
	abi := []byte(`{"Fixture":[]}`)
	require.NoError(t, os.WriteFile(abiPath, abi, 0o644))

	t.Run("ok", func(t *testing.T) {
		confPath := filepath.Join(dir, "conf.yml")
		require.NoError(t, os.WriteFile(confPath, fmt.Appendf(nil, confYAML, abiPath), 0o644))

		conf, err := (&Conf{}).Load(confPath)
		require.NoError(t, err)
		assert.Equal(t, "ws://example", conf.WS.Value())
		assert.Equal(t, "file:test.db?_journal_mode=WAL", conf.DB.Value())
		assert.Equal(t, 50*time.Millisecond, conf.Ethrd.PollInterval)
		require.NotNil(t, conf.Ethrd.Bootstrap)
		assert.Equal(t, abi, conf.Ethrd.Bootstrap.CombinedABI.Value())
	})

	t.Run("missing file", func(t *testing.T) {
		_, err := (&Conf{}).Load(filepath.Join(dir, "nope.yml"))
		assert.Error(t, err)
	})

	t.Run("malformed yaml", func(t *testing.T) {
		confPath := filepath.Join(dir, "bad.yml")
		require.NoError(t, os.WriteFile(confPath, []byte("ws: [unclosed"), 0o644))
		_, err := (&Conf{}).Load(confPath)
		assert.Error(t, err)
	})

	t.Run("malformed redacted", func(t *testing.T) {
		confPath := filepath.Join(dir, "badred.yml")
		require.NoError(t, os.WriteFile(confPath, []byte("ws: {bogus: x}\n"), 0o644))
		_, err := (&Conf{}).Load(confPath)
		assert.ErrorContains(t, err, "expected scalar or {file: path}")
	})
}
