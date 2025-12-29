package chain

import (
	"bytes"
	"context"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	ethereum "github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"

	"github.com/nft_market_go/internal/store"
)

// MarketplaceScanner periodically scans blocks for marketplace events
// (Listed / Cancelled / Sold) and syncs them into the orders table.
type MarketplaceScanner struct {
	client         *ethclient.Client
	contract       common.Address
	abi            abi.ABI
	orderStore     *store.OrderStore
	logger         *log.Logger
	pollInterval   time.Duration
	maxBatchBlocks uint64
	lastLimitLog   time.Time
}

// NewMarketplaceScanner creates a scanner using the NFTMarketplace ABI at docs/NFTMarketplace.abi.json.
func NewMarketplaceScanner(client *ethclient.Client, contractAddr common.Address, orders *store.OrderStore, logger *log.Logger) (*MarketplaceScanner, error) {
	data, err := os.ReadFile("docs/NFTMarketplace.abi.json")
	if err != nil {
		return nil, err
	}

	parsedABI, err := abi.JSON(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}

	if logger == nil {
		logger = log.Default()
	}

	return &MarketplaceScanner{
		client:         client,
		contract:       contractAddr,
		abi:            parsedABI,
		orderStore:     orders,
		logger:         logger,
		pollInterval:   5 * time.Second,
		maxBatchBlocks: 100, // small block range per query to avoid RPC "limit exceeded"
	}, nil
}

// Run starts the scanning loop. It should be run in its own goroutine.
// It starts from the current latest block and only processes new blocks,
// so it does not backfill historical events.
func (s *MarketplaceScanner) Run(ctx context.Context) {
	// Initialize lastScanned to the current head, so we only process new blocks.
	head, err := s.client.HeaderByNumber(ctx, nil)
	if err != nil {
		s.logger.Printf("marketplace scanner: failed to get head block: %v", err)
		return
	}
	lastScanned := head.Number.Uint64()
	s.logger.Printf("marketplace scanner: starting from block %d (no historical backfill)", lastScanned)

	ticker := time.NewTicker(s.pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Printf("marketplace scanner: context canceled, stopping")
			return
		case <-ticker.C:
			// Check latest block number.
			head, err := s.client.HeaderByNumber(ctx, nil)
			if err != nil {
				s.logger.Printf("marketplace scanner: get head error: %v", err)
				continue
			}
			latest := head.Number.Uint64()
			if latest <= lastScanned {
				continue
			}

			// Scan in small batches to avoid RPC "limit exceeded" errors.
			from := lastScanned + 1
			batchSize := s.maxBatchBlocks
			for from <= latest {
				to := from + batchSize - 1
				if to > latest {
					to = latest
				}

				// Only care about Listed / Cancelled / Sold events.
				topics := [][]common.Hash{
					{
						s.abi.Events["Listed"].ID,
						s.abi.Events["Cancelled"].ID,
						s.abi.Events["Sold"].ID,
					},
				}

				query := ethereum.FilterQuery{
					FromBlock: big.NewInt(int64(from)),
					ToBlock:   big.NewInt(int64(to)),
					Addresses: []common.Address{s.contract},
					Topics:    topics,
				}

				logs, err := s.client.FilterLogs(ctx, query)
				if err != nil {
					limitErr := strings.Contains(err.Error(), "limit exceeded")
					if limitErr {
						// Throttle logging for noisy RPC limit errors.
						now := time.Now()
						if now.Sub(s.lastLimitLog) > 5*time.Second {
							s.logger.Printf("marketplace scanner: FilterLogs limit exceeded (from %d to %d)", from, to)
							s.lastLimitLog = now
						}

						// If RPC complains about limits, reduce batch size and retry from the same block.
						if batchSize > 1 {
							batchSize = batchSize / 2
							if batchSize < 1 {
								batchSize = 1
							}
							continue
						}
						// Already at batch size 1 and still hitting limits: skip this block range.
						now = time.Now()
						if now.Sub(s.lastLimitLog) > 5*time.Second {
							s.logger.Printf("marketplace scanner: skipping block range %d-%d due to persistent RPC limit errors", from, to)
							s.lastLimitLog = now
						}
						lastScanned = to
						from = to + 1
						continue
					}

					// Other errors: log once and break this polling cycle; we'll retry next tick.
					s.logger.Printf("marketplace scanner: FilterLogs error (from %d to %d): %v", from, to, err)
					break
				}

				for _, lg := range logs {
					if err := s.handleLog(ctx, lg); err != nil {
						s.logger.Printf("marketplace scanner: handleLog error: %v", err)
					}
				}

				// Successfully processed this batch; advance.
				lastScanned = to
				from = to + 1
			}
		}
	}
}

// ResyncRecent rescans the recent block range [latest-lookback+1, latest]
// and re-applies marketplace events to the orders table. This helps repair
// backend state when some events were missed due to temporary RPC errors
// or downtime. It is safe to call periodically; writes are idempotent.
func (s *MarketplaceScanner) ResyncRecent(ctx context.Context, lookbackBlocks uint64) error {
	head, err := s.client.HeaderByNumber(ctx, nil)
	if err != nil {
		return err
	}
	latest := head.Number.Uint64()
	if latest == 0 {
		return nil
	}

	var from uint64 = 1
	if lookbackBlocks > 0 && lookbackBlocks < latest {
		from = latest - lookbackBlocks + 1
	}

	batchSize := s.maxBatchBlocks

	for from <= latest {
		to := from + batchSize - 1
		if to > latest {
			to = latest
		}

		// Only care about Listed / Cancelled / Sold events.
		topics := [][]common.Hash{
			{
				s.abi.Events["Listed"].ID,
				s.abi.Events["Cancelled"].ID,
				s.abi.Events["Sold"].ID,
			},
		}

		query := ethereum.FilterQuery{
			FromBlock: big.NewInt(int64(from)),
			ToBlock:   big.NewInt(int64(to)),
			Addresses: []common.Address{s.contract},
			Topics:    topics,
		}

		logs, err := s.client.FilterLogs(ctx, query)
		if err != nil {
			limitErr := strings.Contains(err.Error(), "limit exceeded")
			if limitErr {
				// Throttle logging for noisy RPC limit errors.
				now := time.Now()
				if now.Sub(s.lastLimitLog) > 5*time.Second {
					s.logger.Printf("marketplace scanner: reconcile FilterLogs limit exceeded (from %d to %d)", from, to)
					s.lastLimitLog = now
				}

				// If RPC complains about limits, reduce batch size and retry from the same block.
				if batchSize > 1 {
					batchSize = batchSize / 2
					if batchSize < 1 {
						batchSize = 1
					}
					continue
				}
				// Already at batch size 1 and still hitting limits: skip this block range.
				now = time.Now()
				if now.Sub(s.lastLimitLog) > 5*time.Second {
					s.logger.Printf("marketplace scanner: reconcile skipping block range %d-%d due to persistent RPC limit errors", from, to)
					s.lastLimitLog = now
				}
				from = to + 1
				continue
			}

			// Other errors: log once and abort this reconciliation pass; caller can retry later.
			s.logger.Printf("marketplace scanner: reconcile FilterLogs error (from %d to %d): %v", from, to, err)
			return err
		}

		for _, lg := range logs {
			if err := s.handleLog(ctx, lg); err != nil {
				s.logger.Printf("marketplace scanner: reconcile handleLog error: %v", err)
			}
		}

		from = to + 1
	}

	return nil
}

func (s *MarketplaceScanner) handleLog(ctx context.Context, lg types.Log) error {
	if len(lg.Topics) == 0 {
		return nil
	}

	sig := lg.Topics[0]

	switch sig {
	case s.abi.Events["Listed"].ID:
		return s.handleListed(ctx, lg)
	case s.abi.Events["Cancelled"].ID:
		return s.handleCancelled(ctx, lg)
	case s.abi.Events["Sold"].ID:
		return s.handleSold(ctx, lg)
	default:
		// Other events are ignored.
		return nil
	}
}

func (s *MarketplaceScanner) handleListed(ctx context.Context, lg types.Log) error {
	// Indexed topics: [0] event sig, [1] listingId, [2] seller, [3] nft
	if len(lg.Topics) < 4 {
		return nil
	}

	listingID := lg.Topics[1].Big()
	seller := common.HexToAddress(lg.Topics[2].Hex())
	nft := common.HexToAddress(lg.Topics[3].Hex())

	// Non-indexed fields: tokenId, amount, price
	var data struct {
		TokenId *big.Int
		Amount  *big.Int
		Price   *big.Int
	}
	if err := s.abi.UnpackIntoInterface(&data, "Listed", lg.Data); err != nil {
		return err
	}

	order := &store.Order{
		ListingID:  listingID.Int64(),
		Seller:     seller.Hex(),
		Buyer:      "",
		NFTName:    "",
		NFTAddress: nft.Hex(),
		TokenID:    data.TokenId.Int64(),
		Amount:     data.Amount.Int64(),
		Price:      data.Price.String(), // wei string
		Status:     store.OrderStatusListed,
		TxHash:     lg.TxHash.Hex(),
		Deleted:    0,
	}

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	return s.orderStore.Upsert(ctx, order)
}

func (s *MarketplaceScanner) handleCancelled(ctx context.Context, lg types.Log) error {
	if len(lg.Topics) < 2 {
		return nil
	}
	listingID := lg.Topics[1].Big().Int64()

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	// Try to load existing order; if not found, create a minimal one.
	existing, err := s.orderStore.GetByID(ctx, listingID)
	if err != nil {
		// If no row, create a new placeholder.
		order := &store.Order{
			ListingID: listingID,
			Status:    store.OrderStatusCanceled,
			TxHash:    lg.TxHash.Hex(),
			Deleted:   0,
		}
		return s.orderStore.Upsert(ctx, order)
	}

	existing.Status = store.OrderStatusCanceled
	existing.TxHash = lg.TxHash.Hex()
	return s.orderStore.Upsert(ctx, existing)
}

func (s *MarketplaceScanner) handleSold(ctx context.Context, lg types.Log) error {
	if len(lg.Topics) < 3 {
		return nil
	}

	listingID := lg.Topics[1].Big().Int64()
	buyer := common.HexToAddress(lg.Topics[2].Hex())

	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	existing, err := s.orderStore.GetByID(ctx, listingID)
	if err != nil {
		// If no row yet, create a minimal record.
		order := &store.Order{
			ListingID: listingID,
			Buyer:     buyer.Hex(),
			Status:    store.OrderStatusSuccess,
			TxHash:    lg.TxHash.Hex(),
			Deleted:   0,
		}
		return s.orderStore.Upsert(ctx, order)
	}

	existing.Buyer = buyer.Hex()
	existing.Status = store.OrderStatusSuccess
	existing.TxHash = lg.TxHash.Hex()

	return s.orderStore.Upsert(ctx, existing)
}
