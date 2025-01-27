package uniswapv3

import (
	"bytes"
	"context"
	"encoding/gob"
	"errors"
	"math/big"

	univ3pool "github.com/blathering/dex-feed/bindings/uniswap_v3/pool"
	"github.com/blathering/dex-feed/token"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
)

var (
	ErrWrongToken = errors.New("uniswap v3 pool: PriceOf: token not in pool")
)

type PoolOpts struct {
	Token0 token.Token
	Token1 token.Token
	Fee    int64
}

type PoolState struct {
	SqrtPriceX96   *big.Int
	Tick           *big.Int
	Liquidity      *big.Int
	LiquidityGross *big.Int
	LiquidityNet   *big.Int
}

type Pool struct {
	Name    string
	Address common.Address

	// NOTE: this field is empty for now because it is lost on encoding/decoding
	caller *univ3pool.Univ3poolCaller

	Immutables PoolOpts
	State      PoolState // The last known Pool State
}

func NewPool(client *ethclient.Client, name string, poolAddress common.Address, immutables PoolOpts) (*Pool, error) {
	caller, err := univ3pool.NewUniv3poolCaller(poolAddress, client)
	if err != nil {
		return nil, err
	}

	return &Pool{
		Name:       name,
		Address:    poolAddress,
		caller:     caller,
		Immutables: immutables,
		// Initialize empty PoolState
		State: PoolState{},
	}, nil
}

// UpdateState updates the internal pool state. Should be called every time the state changes on-chain
// i.e. on a new block. Note that cached pools should have their states refreshed as well.
// TODO: accept context
func (p *Pool) UpdateState(ctx context.Context, client *ethclient.Client) error {
	opts := &bind.CallOpts{Context: ctx}

	caller, err := univ3pool.NewUniv3poolCaller(p.Address, client)
	if err != nil {
		return err
	}

	slot0, err := caller.Slot0(opts)
	if err != nil {
		return err
	}
	p.State.SqrtPriceX96 = slot0.SqrtPriceX96
	p.State.Tick = slot0.Tick

	liquidity, err := caller.Liquidity(opts)
	if err != nil {
		return err
	}
	p.State.Liquidity = liquidity

	ticks, err := caller.Ticks(opts, big.NewInt(0))
	if err != nil {
		return err
	}
	p.State.LiquidityGross = ticks.LiquidityGross
	p.State.LiquidityNet = ticks.LiquidityNet

	return nil
}

func (p *Pool) PriceOf(token common.Address) (float64, error) {
	var (
		token0Multiplier = new(big.Int).Exp(big.NewInt(10), big.NewInt(p.Immutables.Token0.Decimals), nil)
		token1Multiplier = new(big.Int).Exp(big.NewInt(10), big.NewInt(p.Immutables.Token1.Decimals), nil)
	)

	if token == p.Immutables.Token0.Address {
		numerator := new(big.Int).Exp(p.State.SqrtPriceX96, big.NewInt(2), nil)
		// multiply by token decimals
		numerator = numerator.Mul(numerator, token0Multiplier)
		n := new(big.Float).SetInt(numerator)

		denominator := new(big.Int).Exp(big.NewInt(2), big.NewInt(192), nil)
		d := new(big.Float).SetInt(denominator)

		res := n.Quo(n, d)
		price, _ := res.Quo(res, new(big.Float).SetInt(token1Multiplier)).Float64()
		return price, nil
	} else if token == p.Immutables.Token1.Address {
		numerator := new(big.Int).Exp(big.NewInt(2), big.NewInt(192), nil)
		numerator = numerator.Mul(numerator, token1Multiplier)
		n := new(big.Float).SetInt(numerator)

		denominator := new(big.Int).Exp(p.State.SqrtPriceX96, big.NewInt(2), nil)
		d := new(big.Float).SetInt(denominator)

		res := n.Quo(n, d)
		price, _ := res.Quo(res, new(big.Float).SetInt(token0Multiplier)).Float64()

		return price, nil
	}

	return 0, ErrWrongToken
}

func Decode(poolBytes []byte) (*Pool, error) {
	buf := bytes.NewBuffer(poolBytes)
	dec := gob.NewDecoder(buf)

	var pool Pool
	if err := dec.Decode(&pool); err != nil {
		return nil, err
	}

	return &pool, nil
}

func (p Pool) Encode() ([]byte, error) {
	var buf bytes.Buffer
	enc := gob.NewEncoder(&buf)
	// Problem: gob can only encode exported fields, which univ3pool.Caller has none of. So we can't encode that field.
	if err := enc.Encode(p); err != nil {
		return []byte{}, err
	}

	return buf.Bytes(), nil
}
