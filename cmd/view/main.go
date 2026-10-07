package main

import (
	ethrd "github.com/mpolitzer/ethrd"
	"encoding/json"
	"errors"
	"math"

	"github.com/mpolitzer/ethrd/db"
	"context"
	"database/sql"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"github.com/spf13/pflag"

	_ "modernc.org/sqlite"
)

const (
	chunkSize = 512
)

var (
	oob = errors.New("out of bounds")
	window = db.SelectLogsWindowDescParams{
		BlockNumber: math.MaxInt64,
		LogIndex:    math.MaxInt64,
		BlockHash:   common.Hash{},
		Limit:       chunkSize,
	}
)

type data struct {
	tview.TableContentReadOnly
	q     *db.Queries
	demux ethrd.Demux

	ascl [][]db.Log
	ascw db.SelectLogsWindowAscParams

	descl [][]db.Log
	descw db.SelectLogsWindowDescParams

	table  *tview.Table
	detail *tview.TextView
}

func (d *data) ascFetch(ctx context.Context) {
	logs, err := d.q.SelectLogsWindowAsc(ctx, d.ascw)
	if err != nil {
		panic(err)
	}
	if len(logs) == 0 {
		return
	}
	d.ascl = append(d.ascl, logs)
	last := len(logs) - 1
	d.ascw = db.SelectLogsWindowAscParams{
		BlockNumber: logs[last].BlockNumber,
		LogIndex:    logs[last].LogIndex,
		BlockHash:   logs[last].BlockHash,
		Limit:       chunkSize,
	}
}

func (d *data) descFetch(ctx context.Context) {
	logs, err := d.q.SelectLogsWindowDesc(ctx, d.descw)
	if err != nil {
		panic(err)
	}
	if len(logs) == 0 {
		return
	}
	d.descl = append(d.descl, logs)
	last := len(logs) - 1
	d.descw = db.SelectLogsWindowDescParams{
		BlockNumber: logs[last].BlockNumber,
		LogIndex:    logs[last].LogIndex,
		BlockHash:   logs[last].BlockHash,
		Limit:       chunkSize,
	}
}

func (d *data) at(ctx context.Context, row int) (db.Log, error) {
	zero := db.Log{}
	if row <= d.GetRowCount()/2 {
		div, mod := row/chunkSize, row%chunkSize
		if len(d.descl) <= div {
			d.descFetch(ctx)
		}
		if len(d.descl) <= div || len(d.descl[div]) <= mod {
			return zero, oob
		}
		return d.descl[div][mod], nil
	} else {
		row := d.GetRowCount() - 1 - row
		div, mod := row/chunkSize, row%chunkSize
		if len(d.ascl) <= div {
			d.ascFetch(ctx)
		}
		if len(d.ascl) <= div || len(d.ascl[div]) <= mod {
			return zero, oob
		}
		return d.ascl[div][mod], nil
	}
}

func (d *data) GetCell(row, column int) *tview.TableCell {
	ctx := context.Background()
	log, err := d.at(ctx, row)
	if err != nil {
		return tview.NewTableCell(fmt.Sprintf("%10v%8v%4v%16s", "-", "-", "-", "-"))
	}

	typesLog, _ := log.To()
	key, err := d.demux.ParseKey(typesLog)
	if err != nil {
		return nil
	}

	return tview.NewTableCell(fmt.Sprintf("%10v%8v%4v%16s", log.BlockNumber, log.LogIndex, log.Tags, key))
}

func (d *data) GetRowCount() int {
	return math.MaxInt64
}

func (d *data) GetColumnCount() int {
	return 1
}

func main() {
	dbPath := pflag.StringP("db", "d", "log.db", "path to the ethrd database")
	pflag.Parse()

	sqlDB, err := sql.Open("sqlite", fmt.Sprintf("file:%s?_journal_mode=WAL&_busy_timeout=5000", *dbPath))
	if err != nil {
		panic(err)
	}
	defer sqlDB.Close()

	q := db.New(sqlDB)
	abi, err := q.GetCombinedABI(context.Background())
	if err != nil {
		panic(err)
	}

	demux, err := ethrd.NewDemux([]byte(abi))
	if err != nil {
		panic(err)
	}

	data := &data{
		q:     q,
		demux: demux,

		ascw: db.SelectLogsWindowAscParams{
			BlockNumber: 0,
			LogIndex:    0,
			BlockHash:   common.Hash{},
			Limit:       chunkSize,
		},
		descw: db.SelectLogsWindowDescParams{
			BlockNumber: math.MaxInt64,
			LogIndex:    math.MaxInt64,
			BlockHash:   common.Hash{},
			Limit:       chunkSize,
		},
		table: tview.NewTable().
			SetBorders(false).
			SetSelectable(true, true),
		detail: tview.NewTextView(),
	}
	data.table.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() != tcell.KeyEnter {
			return event
		}
		row, _ := data.table.GetSelection()
		log, err := data.at(context.Background(), row)
		if err != nil {
			return nil
		}
		typesLog, _ := log.To()
		key, val, err := data.demux.Decode(typesLog)
		if err != nil {
			return nil
		}
		bytes, err := json.MarshalIndent(val, "", "\t")
		data.detail.SetText(`"` + key + `": ` + string(bytes))

		return nil
	})

	data.table.SetContent(data)
	grid := tview.NewGrid().
		SetRows(0).
		SetColumns(40, 0).
		SetBorders(true)
	grid.AddItem(data.table, 0, 0, 1, 1, 0, 0, false).
		AddItem(data.detail, 0, 1, 1, 1, 0, 0, false)

	if err := tview.NewApplication().SetRoot(grid, true).SetFocus(data.table).Run(); err != nil {
		panic(err)
	}
}
