package main

import (
	"github.com/mpolitzer/ethrd"

	"github.com/mpolitzer/redacted"
	"context"
	"database/sql"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/lmittmann/tint"
	"github.com/spf13/pflag"

	"go.yaml.in/yaml/v4"

	//_ "github.com/mattn/go-sqlite3"
	_ "modernc.org/sqlite"
)

type Conf struct {
	Ethrd ethrd.Conf             `yaml:"ethrd"`
	WS    redacted.Value[string] `yaml:"ws"`
	DB    redacted.Value[string] `yaml:"db"`
}

type State struct {
	logger *slog.Logger
	ethrd  *ethrd.State
}

func (c *Conf) Load(input string) (*Conf, error) {
	contents, err := os.ReadFile(input)
	if err != nil {
		return c, err
	}
	if err := yaml.Unmarshal(contents, c); err != nil {
		return c, err
	}
	return c, nil
}

func NewLogger(level slog.Level, color bool) *slog.Logger {
	opts := &tint.Options{
		Level:     level,
		AddSource: level == slog.LevelDebug,
		// RFC3339 with milliseconds and without timezone
		TimeFormat: "2006-01-02T15:04:05.000",
		NoColor:    !color,
	}
	handler := tint.NewTextHandler(os.Stdout, opts)
	return slog.New(handler)
}

func main() {
	cPath := pflag.StringP("conf", "c", "conf.yml", "yaml configuration file")
	pflag.Parse()
	if err := run(context.Background(), *cPath); err != nil {
		panic(err)
	}
}

func run(ctx context.Context, confPath string) error {
	startupTime := time.Now()

	conf, err := (&Conf{}).Load(confPath)
	if err != nil {
		return err
	}

	logger := NewLogger(slog.LevelDebug, true)
	ctx, stop := signal.NotifyContext(ctx, syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := sql.Open("sqlite", conf.DB.Value())
	if err != nil {
		return err
	}

	client, err := ethclient.DialContext(ctx, conf.WS.Value())
	if err != nil {
		return err
	}

	ethrdState, err := ethrd.New(ctx, &conf.Ethrd, logger.With("module", "ethrd"), client, db)
	if err != nil {
		return err
	}

	state := &State{
		logger: logger.With("module", "main"),
		ethrd:  ethrdState,
	}

	state.logger.Info("startup",
		"status", "complete",
		"delta_time", time.Since(startupTime))

	for ev, err := range state.ethrd.Run(ctx) {
		if err != nil {
			return err
		}
		state.logger.Debug("event",
			"tags", ev.Tags,
			"block_number", ev.BlockNumber,
			"log_index", ev.LogIndex,
		)
	}
	return nil
}
