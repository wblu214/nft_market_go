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

// ExistsByOwnerAndURL returns true if the given owner already has an asset with the same URL.
func (s *NftAssetStore) ExistsByOwnerAndURL(ctx context.Context, owner, url string) (bool, error) {
	const q = `
SELECT 1
FROM nft_assets
WHERE owner = ? AND url = ? AND deleted = 0
LIMIT 1`

	row := s.db.QueryRowContext(ctx, q, owner, url)
	var dummy int
	if err := row.Scan(&dummy); err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// SoftDeleteByNFT marks an asset row as deleted (deleted = 1) by nft_address + token_id.
// This is used when an NFT has been listed and is now由订单管理，不再作为“可用素材”展示。
func (s *NftAssetStore) SoftDeleteByNFT(ctx context.Context, nftAddress string, tokenID int64) error {
	return softDeleteByNFT(ctx, s.db, nftAddress, tokenID)
}

// SoftDeleteByNFTTx is the transactional variant of SoftDeleteByNFT.
func (s *NftAssetStore) SoftDeleteByNFTTx(ctx context.Context, tx *sql.Tx, nftAddress string, tokenID int64) error {
	return softDeleteByNFT(ctx, tx, nftAddress, tokenID)
}

func softDeleteByNFT(ctx context.Context, exec sqlExecutor, nftAddress string, tokenID int64) error {
	const q = `
UPDATE nft_assets
SET deleted = 1
WHERE nft_address = ? AND token_id = ?`

	_, err := exec.ExecContext(ctx, q, nftAddress, tokenID)
	return err
}

// RestoreByNFT cancels logical deletion (deleted = 0) for a row matched by nft_address + token_id.
// 用于挂单取消后恢复到“我的素材”列表。
func (s *NftAssetStore) RestoreByNFT(ctx context.Context, nftAddress string, tokenID int64) error {
	return restoreByNFT(ctx, s.db, nftAddress, tokenID)
}

// RestoreByNFTTx is the transactional variant of RestoreByNFT.
func (s *NftAssetStore) RestoreByNFTTx(ctx context.Context, tx *sql.Tx, nftAddress string, tokenID int64) error {
	return restoreByNFT(ctx, tx, nftAddress, tokenID)
}

func restoreByNFT(ctx context.Context, exec sqlExecutor, nftAddress string, tokenID int64) error {
	const q = `
UPDATE nft_assets
SET deleted = 0
WHERE nft_address = ? AND token_id = ?`

	_, err := exec.ExecContext(ctx, q, nftAddress, tokenID)
	return err
}

// UpdateOwnerByNFT updates the owner of a given on-chain NFT and clears deleted flag.
// 用于成交后，把 NFT 的归属从卖家切换到买家，并让其出现在买家的素材列表中。
func (s *NftAssetStore) UpdateOwnerByNFT(ctx context.Context, nftAddress string, tokenID int64, newOwner string) error {
	return updateOwnerByNFT(ctx, s.db, nftAddress, tokenID, newOwner)
}

// UpdateOwnerByNFTTx is the transactional variant of UpdateOwnerByNFT.
func (s *NftAssetStore) UpdateOwnerByNFTTx(ctx context.Context, tx *sql.Tx, nftAddress string, tokenID int64, newOwner string) error {
	return updateOwnerByNFT(ctx, tx, nftAddress, tokenID, newOwner)
}

func updateOwnerByNFT(ctx context.Context, exec sqlExecutor, nftAddress string, tokenID int64, newOwner string) error {
	const q = `
UPDATE nft_assets
SET owner = ?, deleted = 0
WHERE nft_address = ? AND token_id = ?`

	_, err := exec.ExecContext(ctx, q, newOwner, nftAddress, tokenID)
	return err
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
WHERE nft_address = ? AND token_id = ?
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
