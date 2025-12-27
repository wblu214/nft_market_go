CREATE TABLE IF NOT EXISTS `nft_assets` (
  `id` BIGINT NOT NULL AUTO_INCREMENT COMMENT 'Primary key',
  `name` VARCHAR(255) NOT NULL COMMENT 'NFT display name',
  `owner` VARCHAR(64) NOT NULL COMMENT 'Owner wallet address',
  `cid` VARCHAR(128) NOT NULL COMMENT 'IPFS CID',
  `url` VARCHAR(512) NOT NULL COMMENT 'IPFS gateway URL',
  `token_id` BIGINT DEFAULT NULL COMMENT 'Minted tokenId (ERC721 tokenId or ERC1155 id)',
  `nft_address` VARCHAR(64) DEFAULT NULL COMMENT 'NFT contract address (ERC721 or ERC1155)',
  `amount` BIGINT DEFAULT NULL COMMENT 'Minted amount (1 for ERC721, >=1 for ERC1155)',
  `deleted` TINYINT NOT NULL DEFAULT 0 COMMENT 'Logical delete flag, 0=normal, 1=deleted',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Create time',
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT 'Update time',
  PRIMARY KEY (`id`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='Uploaded NFT assets (images on IPFS)';
