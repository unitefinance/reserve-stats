package main

import (
	"encoding/csv"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"

	"github.com/urfave/cli"
	"go.uber.org/zap"

	"github.com/KyberNetwork/reserve-stats/accounting/common"
	"github.com/KyberNetwork/reserve-stats/accounting/huobi/storage/postgres"
	withdrawstorage "github.com/KyberNetwork/reserve-stats/accounting/huobi/storage/withdrawal-history/postgres"
	libapp "github.com/KyberNetwork/reserve-stats/lib/app"
	"github.com/KyberNetwork/reserve-stats/lib/huobi"
)

const (
	tradeHistoryFileFlag    = "trade-history-file"
	withdrawHistoryFileFlag = "withdraw-history-file"
)

func main() {
	app := libapp.NewApp()
	app.Name = "Huobi Fetcher"
	app.Usage = "Huobi Fetcher for trade logs"
	app.Action = run
	app.Version = "0.0.1"
	app.Flags = append(app.Flags,
		cli.StringFlag{
			Name:   tradeHistoryFileFlag,
			Usage:  "huobi trade history file",
			EnvVar: "TRADE_HISTORY_FILE",
		},
		cli.StringFlag{
			Name:   withdrawHistoryFileFlag,
			Usage:  "huobi withdraw history file",
			EnvVar: "WITHDRAW_HISTORY_FILE",
		},
	)
	app.Flags = append(app.Flags, libapp.NewPostgreSQLFlags(common.DefaultCexTradesDB)...)
	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}

func currentLocation() string {
	_, fileName, _, _ := runtime.Caller(0)
	return filepath.Dir(fileName)
}

func importTradeHistory(sugar *zap.SugaredLogger, historyFile string, hdb *postgres.HuobiStorage) error {
	var (
		logger      = sugar.With("func", "accounting-huobi-import-data/importTradeHistory")
		types       = []string{"", "buy-market", "sell-market", "buy-limit", "sell-limit"}
		states      = []string{"", "pre-submitted", "submitting", "submitted", "partial-filled", "partial-canceled", "filled", "canceled", ""}
		orderAmount string
	)
	csvFile, err := os.Open(historyFile)
	if err != nil {
		return err
	}
	defer csvFile.Close()

	reader := csv.NewReader(csvFile)
	var tradeHistories []huobi.TradeHistory
	lines, err := reader.ReadAll()
	if err != nil {
		return err
	}
	for id, line := range lines {
		if id == 0 {
			continue
		}
		orderID, err := strconv.ParseInt(line[0], 10, 64)
		if err != nil {
			return err
		}
		logger.Infow("order id", "id", orderID)

		updatedAt, err := strconv.ParseUint(line[9], 10, 64)
		if err != nil {
			return err
		}
		logger.Infow("updated at", "time", updatedAt)

		orderType, err := strconv.ParseInt(line[2], 10, 64)
		if err != nil {
			return err
		}
		logger.Infow("order type", "type", orderType)

		orderState, err := strconv.ParseInt(line[5], 10, 64)
		if err != nil {
			return err
		}

		if len(line) > 10 {
			orderAmount = line[10]
		}

		tradeHistories = append(tradeHistories, huobi.TradeHistory{
			ID:         orderID,
			Symbol:     line[1],
			Source:     "api",
			FinishedAt: updatedAt,
			Type:       types[orderType],
			Price:      line[6],
			State:      states[orderState],
			FieldFees:  line[8],
			Amount:     orderAmount,
		})
	}
	return hdb.UpdateTradeHistory(tradeHistories)
}

func importWithdrawHistory(sugar *zap.SugaredLogger, historyFile string, hdb *withdrawstorage.HuobiStorage) error {
	var (
		logger            = sugar.With("func", "accounting-huobi-import-data/importWithdrawHistory")
		withdrawHistories []huobi.WithdrawHistory
		// withdrawStates    = []string{""}
	)
	logger.Infow("import withdraw history from file", "file", historyFile)

	csvFile, err := os.Open(historyFile)
	if err != nil {
		return err
	}
	defer csvFile.Close()

	reader := csv.NewReader(csvFile)
	lines, err := reader.ReadAll()
	if err != nil {
		return err
	}
	for id, line := range lines {
		if id == 0 {
			continue
		}
		withdrawID, err := strconv.ParseUint(line[0], 10, 64)
		if err != nil {
			return err
		}

		amount, err := strconv.ParseFloat(line[3], 64)
		if err != nil {
			return err
		}

		fee, err := strconv.ParseFloat(line[4], 64)
		if err != nil {
			return err
		}

		updatedAt, err := strconv.ParseUint(line[5], 10, 64)
		if err != nil {
			return err
		}
		logger.Infow("updated at", "time", updatedAt)

		withdrawHistories = append(withdrawHistories, huobi.WithdrawHistory{
			ID:        withdrawID,
			Currency:  line[1],
			Amount:    amount,
			Fee:       fee,
			Type:      "withdraw",
			TxHash:    line[7],
			Address:   line[6],
			UpdatedAt: updatedAt,
			// State:    withdrawStates[state],
		})
	}
	return hdb.UpdateWithdrawHistory(withdrawHistories)
}

func run(c *cli.Context) error {
	if err := libapp.Validate(c); err != nil {
		return err
	}

	sugar, flush, err := libapp.NewSugaredLogger(c)
	if err != nil {
		return err
	}
	defer flush()

	historyFile := c.String(tradeHistoryFileFlag)

	db, err := libapp.NewDBFromContext(c)
	if err != nil {
		return err
	}

	hdb, err := postgres.NewDB(sugar, db)
	if err != nil {
		return err
	}
	if historyFile != "" {
		if err := importTradeHistory(sugar, historyFile, hdb); err != nil {
			return err
		}
	} else {
		sugar.Info("No trade history provided. Skip")
	}

	withdrawFile := c.String(withdrawHistoryFileFlag)

	wdb, err := withdrawstorage.NewDB(sugar, db)
	if err != nil {
		return err
	}

	if withdrawFile != "" {
		if err := importWithdrawHistory(sugar, withdrawFile, wdb); err != nil {
			return err
		}
	} else {
		sugar.Info("No withdraw history file provided. Skip")
	}

	return nil
}
