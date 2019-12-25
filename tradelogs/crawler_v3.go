package tradelogs

import (
	"math/big"
	"time"

	ethereum "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/pkg/errors"

	"github.com/KyberNetwork/reserve-stats/tradelogs/common"
)

func (crawler *Crawler) fetchTradeLogV3(fromBlock, toBlock *big.Int, timeout time.Duration) ([]common.TradeLog, error) {
	var result []common.TradeLog

	topics := [][]ethereum.Hash{
		{
			ethereum.HexToHash(burnFeeEvent),
			ethereum.HexToHash(feeToWalletEvent),
			ethereum.HexToHash(kyberTradeEvent),
		},
	}

	typeLogs, err := crawler.fetchLogsWithTopics(fromBlock, toBlock, timeout, topics)
	if err != nil {
		return nil, errors.Wrap(err, "failed to fetch log by topic")
	}

	result, err = crawler.assembleTradeLogsV3(typeLogs)
	if err != nil {
		return nil, err
	}

	return result, nil
}

func (crawler *Crawler) assembleTradeLogsV3(eventLogs []types.Log) ([]common.TradeLog, error) {
	var (
		result   []common.TradeLog
		tradeLog common.TradeLog
		err      error
	)

	for _, log := range eventLogs {
		if log.Removed {
			continue // Removed due to chain reorg
		}

		if len(log.Topics) == 0 {
			return result, errors.New("log item has no topic")
		}

		tradeLog.TxSender, err = crawler.getTxSender(log, defaultTimeout)
		if err != nil {
			return result, errors.New("could not get trade log sender")
		}

		topic := log.Topics[0]
		switch topic.Hex() {
		case feeToWalletEvent:
			if tradeLog, err = fillWalletFees(tradeLog, log); err != nil {
				return nil, err
			}
		case burnFeeEvent:
			if tradeLog, err = fillBurnFees(tradeLog, log); err != nil {
				return nil, err
			}
		case kyberTradeEvent:
			if tradeLog, err = fillKyberTradeV3(tradeLog, log, crawler.volumeExludedReserves); err != nil {
				return nil, err
			}
			if tradeLog.Timestamp, err = crawler.txTime.Resolve(log.BlockNumber); err != nil {
				return nil, errors.Wrapf(err, "failed to resolve timestamp by block_number %v", log.BlockNumber)
			}

			crawler.sugar.Infow("gathered new trade log", "trade_log", tradeLog)
			// one trade only has one and only ExecuteTrade event
			result = append(result, tradeLog)
			tradeLog = common.TradeLog{}
		default:
			return nil, errUnknownLogTopic
		}
	}

	return result, nil
}
