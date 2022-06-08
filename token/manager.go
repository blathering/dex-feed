package token

import (
	"context"
	"errors"
	"fmt"
	"path"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/blathering/dex-feed/bindings/erc20"
	"github.com/blathering/dex-feed/db"
)

var (
	ErrNoExist      = errors.New("token doesn't exist")
	ErrNotConnected = errors.New("not connected to a chain, use Connect method")
)

type TokenManager interface {
	GetToken(ctx context.Context, address common.Address) (Token, error)
}

type tokenManager struct {
	client *ethclient.Client
	cache  db.Cacher
}

func NewTokenDB(client *ethclient.Client, opts db.Opts) TokenManager {
	var c db.Cacher

	if opts.CacheSize == 0 {
		opts.CacheSize = 2048
	}

	c = db.NewMemoryCache(opts.CacheSize)

	if opts.Persistent {
		c = db.NewFullCache(path.Join(opts.DataDir, "token_cache"), opts.CacheSize)
	}

	return &tokenManager{
		cache:  c,
		client: client,
	}
}

// Adds a token to the cache
func (tc *tokenManager) add(token Token) {
	// We can ignore the error here
	encoded, _ := token.Encode()
	tc.cache.Put(token.Address.String(), encoded)
}

// Gets cached token by address if it's present.
func (tc tokenManager) get(address common.Address) (Token, bool) {
	if encoded, ok := tc.cache.Get(address.String()); ok {
		t, err := Decode(encoded)
		if err != nil {
			return Token{}, false
		}

		return t, true
	}

	return Token{}, false
}

func (tc *tokenManager) GetToken(ctx context.Context, address common.Address) (Token, error) {
	// Check cache
	if token, ok := tc.get(address); ok {
		return token, nil
	}

	// Check if we're connected to a chain
	if tc.client == nil {
		return Token{}, ErrNotConnected
	}

	token, err := erc20.NewErc20Caller(address, tc.client)
	if err != nil {
		return Token{}, fmt.Errorf("getting token: %w", err)
	}

	opts := &bind.CallOpts{Context: ctx}
	sym, err := token.Symbol(opts)
	if err != nil {
		return Token{}, fmt.Errorf("getting token: reading name: %w", err)
	}

	decimals, err := token.Decimals(opts)
	if err != nil {
		return Token{}, fmt.Errorf("getting token: reading decimals: %w", err)
	}

	newToken := Token{
		Address:  address,
		Symbol:   sym,
		Decimals: int64(decimals),
	}

	tc.add(newToken)

	return newToken, nil
}
