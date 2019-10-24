package accounting

import (
	"sort"
	"time"

	"github.com/FactomWyomingEntity/private-pool/difficulty"
	"github.com/shopspring/decimal"
)

type Payouts struct {
	Reward // All the reward info

	// PoolFeeRate is the pool cut
	PoolFeeRate decimal.Decimal `sql:"type:decimal(20,8);"`
	PoolFee     int64           // In PEG
	// Dust should always be 0, but it is any rewards that are not accounted
	// to a user or to the pool. We should account for it if it happens.
	Dust int64

	PoolDifficuty float64
	TotalHashrate float64 `gorm:"default:0"`

	UserPayouts []UserPayout `gorm:"foreignkey:JobID"`
}

func NewPayout(r Reward, poolFeeRate decimal.Decimal, work ShareMap) *Payouts {
	p := new(Payouts)
	p.PoolFeeRate = poolFeeRate
	p.Reward = r
	remaining := p.TakePoolCut(p.Reward.PoolReward)
	p.Payouts(work, remaining)

	return p
}

func (p *Payouts) Payouts(work ShareMap, remaining int64) {
	p.PoolDifficuty = work.TotalDiff
	var totalPayout int64
	for user, work := range work.Sums {
		prop := decimal.NewFromFloat(work.TotalDifficulty).Div(decimal.NewFromFloat(p.PoolDifficuty))
		prop = prop.Truncate(AccountingPrecision)

		// Estimate user hashrate
		last := 20
		if work.TotalShares < 20 {
			last = work.TotalShares
		}

		target := work.Targets[last-1]
		hashrate := difficulty.EffectiveHashRate(target, last, work.LastShare.Sub(work.FirstShare).Seconds())

		pay := UserPayout{
			UserID:           user,
			UserDifficuty:    work.TotalDifficulty,
			TotalSubmissions: work.TotalShares,
			Proportion:       prop,
			Payout:           cut(remaining, prop),
			HashRate:         hashrate,
		}
		p.UserPayouts = append(p.UserPayouts, pay)
		totalPayout += pay.Payout

		// Only if a miner mines for at least 20s
		if work.LastShare.Sub(work.FirstShare) > time.Second*20 {
			p.TotalHashrate += pay.HashRate
		}
	}
	p.Dust = remaining - totalPayout
}

// TakePoolCut will take the amount owed the pool, and return the
// remaining rewards to be distributed
func (p *Payouts) TakePoolCut(remaining int64) int64 {
	if p.PoolFeeRate.IsZero() {
		return remaining
	}

	p.PoolFee = cut(remaining, p.PoolFeeRate)
	return remaining - p.PoolFee
}

// cut returns the proportional amount in the total
func cut(total int64, prop decimal.Decimal) int64 {
	amt := decimal.New(total, 0)
	cut := amt.Mul(prop)
	return cut.IntPart()
}

type UserPayout struct {
	JobID            int32  `gorm:"primary_key"`
	UserID           string `gorm:"primary_key"`
	UserDifficuty    float64
	TotalSubmissions int

	// Proportion denoted with 10000 being 100% and 1 being 0.01%
	Proportion decimal.Decimal `sql:"type:decimal(20,8);"`
	Payout     int64           // In PEG

	HashRate float64 `gorm:"default:0"` // Hashrate in h/s
}

type Reward struct {
	JobID      int32 `gorm:"primary_key"` // Block height of reward payout
	PoolReward int64 // PEG reward for block

	Winning int // Number of oprs in the winning set
	Graded  int // Number of oprs in the graded set
}

// Share is an accepted piece of work done by a miner.
type Share struct {
	JobID      int32  // JobID's are always a block height
	Nonce      []byte // Nonce is the work computed by the miner
	Difficulty float64
	Target     uint64
	Accepted   bool // Shares can be rejected

	// MinerID is the unique ID of the miner that submit the share
	MinerID string
	// All minerID's should be linked to a user via a userid. The userid is who earns the payouts
	UserID string
}

type ShareMap struct {
	// Sealed means no new shares are accepted and we can garbage collect
	Sealed bool

	TotalDiff float64
	Sums      map[string]*ShareSum
}

func NewShareMap() *ShareMap {
	s := new(ShareMap)
	s.Sums = make(map[string]*ShareSum)
	return s
}

func (m *ShareMap) Seal() {
	m.Sealed = true
}

func (m *ShareMap) AddShare(key string, s Share) {
	if m.Sealed {
		return // Do nothing, it's already sealed
	}

	m.TotalDiff += s.Difficulty
	if _, ok := m.Sums[key]; !ok {
		m.Sums[key] = new(ShareSum)
	}
	m.Sums[key].AddShare(s)
}

// ShareSum is the sum of shares for a given job
type ShareSum struct {
	TotalDifficulty float64
	TotalShares     int

	FirstShare time.Time
	LastShare  time.Time
	Targets    [20]uint64
}

func (sum *ShareSum) AddShare(s Share) {
	if sum.FirstShare.IsZero() {
		sum.FirstShare = time.Now()
	}
	sum.LastShare = time.Now()

	sum.TotalDifficulty += s.Difficulty
	sum.TotalShares++
	InsertTarget(s.Target, &sum.Targets, sum.TotalShares)
}

func InsertTarget(t uint64, a *[20]uint64, total int) {
	if total > 20 {
		total = 20
	}
	index := sort.Search(total, func(i int) bool { return a[i] < t })
	if index == 20 {
		return
	}
	// Move things down
	copy(a[index+1:], a[index:])
	// Insert at index
	a[index] = t
}

func InsertSorted(s []int, e int) []int {
	s = append(s, 0)
	i := sort.Search(len(s), func(i int) bool { return s[i] > e })
	copy(s[i+1:], s[i:])
	s[i] = e
	return s
}

// some utils
func TruncateTo4(v float64) float64 {
	return float64(int64(v*1e4)) / 1e4
}
