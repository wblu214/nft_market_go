package store

import (
	"context"
	"database/sql"
	"os"
	"time"
)

// NftAsset represents a row in the nft_assets table.
// It mirrors the schema defined in sql/create_nft_assets_table.sql.
type NftAsset struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Owner      string    `json:"owner"`
	CID        string    `json:"cid"`
	URL        string    `json:"url"`
	TokenID    int64     `json:"token_id"`    // 0 means "not minted yet" when NULL in DB
	NFTAddress string    `json:"nft_address"` // empty when NULL in DB
	Amount     int64     `json:"amount"`      // 0 when NULL in DB
	Deleted    int8      `json:"deleted"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// NftAssetStore wraps access to the nft_assets table in MySQL.
type NftAssetStore struct {
	db *sql.DB
}

// NewNftAssetStore creates a new NftAssetStore.
func NewNftAssetStore(db *sql.DB) *NftAssetStore {
	return &NftAssetStore{db: db}
}

// InitSchema ensures the nft_assets table exists using the provided SQL file.
func (s *NftAssetStore) InitSchema(ctx context.Context) error {
	content, err := os.ReadFile("sql/create_nft_assets_table.sql")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, string(content))
	return err
}

// Insert creates a new nft_assets row. It returns the auto-incremented ID.
func (s *NftAssetStore) Insert(ctx context.Context, a *NftAsset) (int64, error) {
	const q = `
INSERT INTO nft_assets (
  name, owner, cid, url, token_id, nft_address, amount, deleted
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := s.db.ExecContext(ctx, q,
		a.Name,
		a.Owner,
		a.CID,
		a.URL,
		// token_id, nft_address, amount can be nil in DB; here we treat zero/empty as "not set".
		sql.NullInt64{Int64: a.TokenID, Valid: a.TokenID != 0},
		sql.NullString{String: a.NFTAddress, Valid: a.NFTAddress != ""},
		sql.NullInt64{Int64: a.Amount, Valid: a.Amount != 0},
		a.Deleted,
	)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	return id, nil
}

// GetByID returns a single asset by primary key ID.
func (s *NftAssetStore) GetByID(ctx context.Context, id int64) (*NftAsset, error) {
	const q = `
SELECT
  id,
  name,
  owner,
  cid,
  url,
  IFNULL(token_id, 0)    AS token_id,
  IFNULL(nft_address, '') AS nft_address,
  IFNULL(amount, 0)      AS amount,
  deleted,
  created_at,
  updated_at
FROM nft_assets
WHERE id = ?`

	row := s.db.QueryRowContext(ctx, q, id)
	var a NftAsset
	if err := row.Scan(
		&a.ID,
		&a.Name,
		&a.Owner,
		&a.CID,
		&a.URL,
		&a.TokenID,
		&a.NFTAddress,
		&a.Amount,
		&a.Deleted,
		&a.CreatedAt,
		&a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}

// ListByOwner returns recent, undeleted assets owned by the specified address.
func (s *NftAssetStore) ListByOwner(ctx context.Context, owner string, limit int) ([]*NftAsset, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `
SELECT
  id,
  name,
  owner,
  cid,
  url,
  IFNULL(token_id, 0)    AS token_id,
  IFNULL(nft_address, '') AS nft_address,
  IFNULL(amount, 0)      AS amount,
  deleted,
  created_at,
  updated_at
FROM nft_assets
WHERE owner = ? AND deleted = 0
ORDER BY updated_at DESC
LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, owner, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*NftAsset
	for rows.Next() {
		var a NftAsset
		if err := rows.Scan(
			&a.ID,
			&a.Name,
			&a.Owner,
			&a.CID,
			&a.URL,
			&a.TokenID,
			&a.NFTAddress,
			&a.Amount,
			&a.Deleted,
			&a.CreatedAt,
			&a.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, &a)
	}
	return out, rows.Err()
}

// UpdateMintInfo updates token_id, nft_address and amount after on-chain mint.
func (s *NftAssetStore) UpdateMintInfo(ctx context.Context, id int64, tokenID int64, nftAddress string, amount int64) error {
	const q = `
UPDATE nft_assets
SET token_id = ?, nft_address = ?, amount = ?
WHERE id = ? AND deleted = 0`

	_, err := s.db.ExecContext(ctx, q, tokenID, nftAddress, amount, id)
	return err
}

// GetByNFT returns an asset matched by nft_address + token_id.
func (s *NftAssetStore) GetByNFT(ctx context.Context, nftAddress string, tokenID int64) (*NftAsset, error) {
	const q = `
SELECT
  id,
  name,
  owner,
  cid,
  url,
  IFNULL(token_id, 0)     AS token_id,
  IFNULL(nft_address, '') AS nft_address,
  IFNULL(amount, 0)       AS amount,
  deleted,
  created_at,
  updated_at
FROM nft_assets
WHERE nft_address = ? AND token_id = ? AND deleted = 0
LIMIT 1`

	row := s.db.QueryRowContext(ctx, q, nftAddress, tokenID)
	var a NftAsset
	if err := row.Scan(
		&a.ID,
		&a.Name,
		&a.Owner,
		&a.CID,
		&a.URL,
		&a.TokenID,
		&a.NFTAddress,
		&a.Amount,
		&a.Deleted,
		&a.CreatedAt,
		&a.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &a, nil
}
