package chain

import (
	"bytes"
	"context"
	"log"
	"math/big"
	"os"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethereum"

	"github.com/nft_market_go/internal/store"
)

// MarketplaceScanner periodically scans blocks for marketplace events
// (Listed / Cancelled / Sold) and syncs them into the orders table.
type MarketplaceScanner struct {
	client       *ethclient.Client
	contract     common.Address
	abi          abi.ABI
	orderStore   *store.OrderStore
	logger       *log.Logger
	pollInterval time.Duration
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
		client:       client,
		contract:     contractAddr,
		abi:          parsedABI,
		orderStore:   orders,
		logger:       logger,
		pollInterval: 5 * time.Second,
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

			from := lastScanned + 1
			to := latest

			query := ethereum.FilterQuery{
				FromBlock: big.NewInt(int64(from)),
				ToBlock:   big.NewInt(int64(to)),
				Addresses: []common.Address{s.contract},
			}

			logs, err := s.client.FilterLogs(ctx, query)
			if err != nil {
				s.logger.Printf("marketplace scanner: FilterLogs error (from %d to %d): %v", from, to, err)
				continue
			}

			for _, lg := range logs {
				if err := s.handleLog(ctx, lg); err != nil {
					s.logger.Printf("marketplace scanner: handleLog error: %v", err)
				}
				if lg.BlockNumber > lastScanned {
					lastScanned = lg.BlockNumber
				}
			}
		}
	}
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
