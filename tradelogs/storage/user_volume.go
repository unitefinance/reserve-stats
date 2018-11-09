package storage

import (
	"fmt"
	"strings"

	"github.com/KyberNetwork/reserve-stats/lib/timeutil"
	"github.com/KyberNetwork/reserve-stats/tradelogs/common"

	ethereum "github.com/ethereum/go-ethereum/common"
)

//GetUserVolume return volume of an address from time to time by a frequency
func (is *InfluxStorage) GetUserVolume(userAddress ethereum.Address, from, to uint64, freq string) (map[uint64]common.UserVolume, error) {
	var (
		userAddr    = userAddress.Hex()
		measurement string
	)

	result := map[uint64]common.UserVolume{}

	logger := is.sugar.With("user address", userAddr, "freq", freq,
		"fromTime", from, "toTime", to)

	switch strings.ToLower(freq) {
	case day:
		measurement = "user_volume_day"
	case hour:
		measurement = "user_volume_hour"
	}

	q := fmt.Sprintf(`
		SELECT eth_volume, usd_volume from "%s"
		WHERE user_addr = '%s' AND time >= %d%s AND time <= %d%s
	`, measurement, userAddr, from, timePrecision, to, timePrecision)

	logger.Debug(q)

	res, err := is.queryDB(is.influxClient, q)
	if err != nil {
		logger.Error(fmt.Sprintf("cannot query user volume from influx: %s", err.Error()))
		return result, err
	}

	if len(res[0].Series) == 0 {
		return result, nil
	}

	for _, row := range res[0].Series[0].Values {
		ts, ethAmount, usdAmount, err := is.rowToAggregatedUserVolume(row)
		if err != nil {
			return nil, err
		}
		key := timeutil.TimeToTimestampMs(ts)
		result[key] = common.UserVolume{
			ETHAmount: ethAmount,
			USDAmount: usdAmount,
		}
	}

	return result, err
}
