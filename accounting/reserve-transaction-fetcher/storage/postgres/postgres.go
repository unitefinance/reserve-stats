package postgres

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/jmoiron/sqlx"
	"go.uber.org/zap"

	"github.com/KyberNetwork/reserve-stats/accounting/reserve-transaction-fetcher/common"
	"github.com/KyberNetwork/reserve-stats/lib/pgsql"
	"github.com/KyberNetwork/reserve-stats/lib/timeutil"
)

var defaultTableNames = &tableNames{
	Normal:   "rsv_tx_normal",
	Internal: "rsv_tx_internal",
	ERC20:    "rsv_tx_erc20",
}

// tableNames contains name of all PostgreSQL tables used for this this.
type tableNames struct {
	Normal   string
	Internal string
	ERC20    string
}

// Storage is the an implementation of storage.ReserveTransactionStorage interface using PostgresQL.
type Storage struct {
	sugar      *zap.SugaredLogger
	db         *sqlx.DB
	tableNames *tableNames
}

// Option is an configuration option of Storage constructor.
type Option func(*Storage)

// WithTableName is the option to use a non-default table name.
func WithTableName(tn *tableNames) Option {
	return func(s *Storage) { s.tableNames = tn }
}

// NewStorage creates new instance of Storage.
func NewStorage(sugar *zap.SugaredLogger, db *sqlx.DB, options ...Option) (*Storage, error) {
	var (
		logger = sugar.With("func", "accounting/reserve-transaction-fetcher/storage/postgres/NewStorage")
	)
	const schemaFmt = `CREATE TABLE IF NOT EXISTS "%[1]s"
(
    tx_hash text  NOT NULL PRIMARY KEY,
    data    JSONB NOT NULL
);
CREATE INDEX IF NOT EXISTS "%[1]s_time_idx" ON "%[1]s" ((data ->> 'timestamp'));

CREATE TABLE IF NOT EXISTS "%[2]s"
(
    data JSONB NOT NULL UNIQUE
);

CREATE INDEX IF NOT EXISTS "%[2]s_time_idx" ON "%[2]s" ((data ->> 'timestamp'));

CREATE TABLE IF NOT EXISTS "%[3]s"
(
    data JSONB NOT NULL UNIQUE
);

CREATE INDEX IF NOT EXISTS "%[3]s_time_idx" ON "%[3]s" ((data ->> 'timestamp'));
`

	s := &Storage{sugar: sugar, db: db}
	for _, option := range options {
		option(s)
	}
	if s.tableNames == nil {
		s.tableNames = defaultTableNames
	}

	query := fmt.Sprintf(schemaFmt, s.tableNames.Normal, s.tableNames.Internal, s.tableNames.ERC20)
	logger.Infow("initializing database schema", "query", query)
	if _, err := db.Exec(query); err != nil {
		return nil, err
	}
	return s, nil
}

// TearDown removes all in used tables of reserve transaction storage.
func (s *Storage) TearDown() error {
	var logger = s.sugar.With("func", "accounting/reserve-transaction-fetcher/storage/postgres/Storage.TearDown")
	const dropFMT = `
	DROP TABLE %[1]s CASCADE;
	DROP TABLE %[2]s CASCADE;
	DROP TABLE %[3]s CASCADE;
	`
	query := fmt.Sprintf(dropFMT, s.tableNames.Normal, s.tableNames.Internal, s.tableNames.ERC20)
	logger.Debugw("cleanup database", "query", query)
	_, err := s.db.Exec(query)
	return err
}

func (s *Storage) StoreNormalTx(txs []common.NormalTx) (err error) {
	var (
		logger = s.sugar.With("func", "accounting/reserve-transaction-fetcher/storage/postgres/Storage.StoreNormalTx")
	)
	const updateStmt = `INSERT INTO "%[1]s"(tx_hash, data)
VALUES ($1, $2)
ON CONFLICT ON CONSTRAINT "%[1]s_pkey" DO UPDATE SET data = EXCLUDED.data;
`

	query := fmt.Sprintf(updateStmt, s.tableNames.Normal)
	logger.Debugw("storing normal transactions to database", "query", query)

	tx, err := s.db.Beginx()
	if err != nil {
		return
	}
	defer pgsql.CommitOrRollback(tx, logger, &err)
	for _, t := range txs {
		var data []byte
		data, err = json.Marshal(t)
		if err != nil {
			return
		}
		_, err = tx.Exec(query, t.BlockHash, data)
		if err != nil {
			return
		}
	}

	return nil
}

func (s *Storage) GetNormalTx(from time.Time, to time.Time) ([]common.NormalTx, error) {
	var (
		logger = s.sugar.With(
			"func", "accounting/reserve-transaction-fetcher/storage/postgres/Storage.GetNormalTx",
			"from", from.String(),
			"to", to.String(),
		)
		dbResult [][]byte
		results  []common.NormalTx
		t        common.NormalTx
	)
	const selectStmt = `SELECT data
FROM "%[1]s"
WHERE data ->> 'timestamp' >= $1
  AND data ->> 'timestamp' < $2`
	query := fmt.Sprintf(selectStmt, s.tableNames.Normal)
	logger.Debugw("querying normal transactions from database", "query", query)
	if err := s.db.Select(
		&dbResult,
		query,
		timeutil.TimeToTimestampMs(from),
		timeutil.TimeToTimestampMs(to)); err != nil {
		return nil, err
	}
	for _, data := range dbResult {
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, err
		}
		results = append(results, t)
	}
	return results, nil
}

func (s *Storage) StoreInternalTx(txs []common.InternalTx) (err error) {
	var logger = s.sugar.With(
		"func", "accounting/reserve-transaction-fetcher/storage/postgres/Storage.StoreInternalTx",
	)

	const updateStmt = `INSERT INTO "%[1]s"(data)
VALUES ($1)
ON CONFLICT DO NOTHING;
`

	query := fmt.Sprintf(updateStmt, s.tableNames.Internal)
	logger.Debugw("storing internal transactions to database", "query", query)

	tx, err := s.db.Beginx()
	if err != nil {
		return
	}
	defer pgsql.CommitOrRollback(tx, logger, &err)
	for _, t := range txs {
		var data []byte
		data, err = json.Marshal(t)
		if err != nil {
			return
		}

		if _, err = tx.Exec(query, data); err != nil {
			return
		}
	}
	return
}

func (s *Storage) GetInternalTx(from time.Time, to time.Time) ([]common.InternalTx, error) {
	var (
		logger = s.sugar.With(
			"func", "accounting/reserve-transaction-fetcher/storage/postgres/Storage.GetInternalTx",
			"from", from.String(),
			"to", to.String(),
		)
		dbResult [][]byte
		results  []common.InternalTx
		t        common.InternalTx
	)
	const selectStmt = `SELECT data
FROM "%[1]s"
WHERE data ->> 'timestamp' >= $1
  AND data ->> 'timestamp' < $2`
	query := fmt.Sprintf(selectStmt, s.tableNames.Internal)
	logger.Debugw("querying internal transactions from database", "query", query)
	if err := s.db.Select(
		&dbResult,
		query,
		timeutil.TimeToTimestampMs(from),
		timeutil.TimeToTimestampMs(to)); err != nil {
		return nil, err
	}
	for _, data := range dbResult {
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, err
		}
		results = append(results, t)
	}
	return results, nil
}

func (s *Storage) StoreERC20Transfer(txs []common.ERC20Transfer) (err error) {
	var logger = s.sugar.With(
		"func", "accounting/reserve-transaction-fetcher/storage/postgres/Storage.StoreERC20Transfer",
	)

	const updateStmt = `INSERT INTO "%[1]s"(data)
VALUES ($1)
ON CONFLICT DO NOTHING;
`

	query := fmt.Sprintf(updateStmt, s.tableNames.ERC20)
	logger.Debugw("storing ERC20 transfers to database", "query", query)

	tx, err := s.db.Beginx()
	if err != nil {
		return
	}
	defer pgsql.CommitOrRollback(tx, logger, &err)
	for _, t := range txs {
		var data []byte
		data, err = json.Marshal(t)
		if err != nil {
			return
		}

		if _, err = tx.Exec(query, data); err != nil {
			return
		}
	}
	return
}

func (s *Storage) GetERC20Transfer(from time.Time, to time.Time) ([]common.ERC20Transfer, error) {
	var (
		logger = s.sugar.With(
			"func", "accounting/reserve-transaction-fetcher/storage/postgres/Storage.GetERC20Transfer",
			"from", from.String(),
			"to", to.String(),
		)
		dbResult [][]byte
		results  []common.ERC20Transfer
		t        common.ERC20Transfer
	)
	const selectStmt = `SELECT data
FROM "%[1]s"
WHERE data ->> 'timestamp' >= $1
  AND data ->> 'timestamp' < $2`
	query := fmt.Sprintf(selectStmt, s.tableNames.ERC20)
	logger.Debugw("querying ERC20 transfers from database", "query", query)
	if err := s.db.Select(
		&dbResult,
		query,
		timeutil.TimeToTimestampMs(from),
		timeutil.TimeToTimestampMs(to)); err != nil {
		return nil, err
	}
	for _, data := range dbResult {
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, err
		}
		results = append(results, t)
	}
	return results, nil
}
