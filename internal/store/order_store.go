package store

import (
	"context"
	"database/sql"
	"os"
	"time"
)

// OrderStatus represents the lifecycle state of a marketplace listing.
type OrderStatus string

const (
	OrderStatusInit     OrderStatus = "INIT"
	OrderStatusListed   OrderStatus = "LISTED"
	OrderStatusLocked   OrderStatus = "LOCKED"
	OrderStatusSettling OrderStatus = "SETTLING"
	OrderStatusSuccess  OrderStatus = "SUCCESS"
	OrderStatusFailed   OrderStatus = "FAILED"
	OrderStatusCanceled OrderStatus = "CANCELED"
)

// Order mirrors a listing on-chain and is updated via contract events.
type Order struct {
	OrderID    int64       `json:"order_id"`
	ListingID  int64       `json:"listing_id"`
	Seller     string      `json:"seller"`
	Buyer      string      `json:"buyer"`
	NFTName    string      `json:"nft_name"`
	NFTAddress string      `json:"nft_address"`
	URL        string      `json:"url"`
	TokenID    int64       `json:"token_id"`
	Amount     int64       `json:"amount"`
	Price      string      `json:"price"` // wei, matches DECIMAL(36,0)
	Status     OrderStatus `json:"status"`
	TxHash     string      `json:"tx_hash"`
	Deleted    int8        `json:"deleted"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

// OrderStore wraps access to the orders table in MySQL.
type OrderStore struct {
	db *sql.DB
}

// NewOrderStore creates a new OrderStore.
func NewOrderStore(db *sql.DB) *OrderStore {
	return &OrderStore{db: db}
}

// InitSchema ensures the orders table exists.
func (s *OrderStore) InitSchema(ctx context.Context) error {
	// Use the DDL from the provided SQL file to ensure schema matches exactly.
	content, err := os.ReadFile("sql/create_orders_table.sql")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, string(content))
	return err
}

// Upsert creates or updates an order row.
func (s *OrderStore) Upsert(ctx context.Context, o *Order) error {
	return upsertOrder(ctx, s.db, o)
}

// UpsertTx creates or updates an order row using the provided transaction.
func (s *OrderStore) UpsertTx(ctx context.Context, tx *sql.Tx, o *Order) error {
	return upsertOrder(ctx, tx, o)
}

func upsertOrder(ctx context.Context, exec sqlExecutor, o *Order) error {
	const q = `
INSERT INTO orders (
  listing_id, seller, buyer, nft_name, nft_address,
  url, token_id, amount, price, status, tx_hash, deleted
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON DUPLICATE KEY UPDATE
  seller = VALUES(seller),
  buyer = VALUES(buyer),
  nft_name = VALUES(nft_name),
  nft_address = VALUES(nft_address),
  url = VALUES(url),
  token_id = VALUES(token_id),
  amount = VALUES(amount),
  price = VALUES(price),
  status = VALUES(status),
  tx_hash = VALUES(tx_hash),
  deleted = VALUES(deleted);`

	_, err := exec.ExecContext(ctx, q,
		o.ListingID,
		o.Seller,
		o.Buyer,
		o.NFTName,
		o.NFTAddress,
		o.URL,
		o.TokenID,
		o.Amount,
		o.Price,
		o.Status,
		o.TxHash,
		o.Deleted,
	)
	return err
}

// GetByID returns a single order by listing ID.
func (s *OrderStore) GetByID(ctx context.Context, listingID int64) (*Order, error) {
	return getOrderByID(ctx, s.db, listingID, false)
}

// GetByIDForUpdateTx returns a single order by listing ID and locks the row
// for update within the given transaction.
func (s *OrderStore) GetByIDForUpdateTx(ctx context.Context, tx *sql.Tx, listingID int64) (*Order, error) {
	return getOrderByID(ctx, tx, listingID, true)
}

func getOrderByID(ctx context.Context, exec sqlExecutor, listingID int64, forUpdate bool) (*Order, error) {
	const baseQuery = `
SELECT
  order_id,
  IFNULL(listing_id, 0) AS listing_id,
  seller,
  IFNULL(buyer, '') AS buyer,
  IFNULL(nft_name, '') AS nft_name,
  nft_address,
  IFNULL(url, '') AS url,
  token_id,
  amount,
  price,
  status,
  IFNULL(tx_hash, '') AS tx_hash,
  deleted,
  created_at,
  updated_at
FROM orders WHERE listing_id = ?`

	q := baseQuery
	if forUpdate {
		q = q + " FOR UPDATE"
	}

	row := exec.QueryRowContext(ctx, q, listingID)
	var o Order
	if err := row.Scan(
		&o.OrderID,
		&o.ListingID,
		&o.Seller,
		&o.Buyer,
		&o.NFTName,
		&o.NFTAddress,
		&o.URL,
		&o.TokenID,
		&o.Amount,
		&o.Price,
		&o.Status,
		&o.TxHash,
		&o.Deleted,
		&o.CreatedAt,
		&o.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &o, nil
}

// ListRecent returns a small set of recent orders for demo purposes.
func (s *OrderStore) ListRecent(ctx context.Context, limit int) ([]*Order, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT
  order_id,
  IFNULL(listing_id, 0) AS listing_id,
  seller,
  IFNULL(buyer, '') AS buyer,
  IFNULL(nft_name, '') AS nft_name,
  nft_address,
  IFNULL(url, '') AS url,
  token_id,
  amount,
  price,
  status,
  IFNULL(tx_hash, '') AS tx_hash,
  deleted,
  created_at,
  updated_at
FROM orders
WHERE deleted = 0
ORDER BY updated_at DESC
LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(
			&o.OrderID,
			&o.ListingID,
			&o.Seller,
			&o.Buyer,
			&o.NFTName,
			&o.NFTAddress,
			&o.URL,
			&o.TokenID,
			&o.Amount,
			&o.Price,
			&o.Status,
			&o.TxHash,
			&o.Deleted,
			&o.CreatedAt,
			&o.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, &o)
	}
	return out, rows.Err()
}
