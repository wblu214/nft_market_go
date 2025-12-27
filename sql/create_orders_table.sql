CREATE TABLE IF NOT EXISTS `orders` (
  `order_id` BIGINT NOT NULL AUTO_INCREMENT COMMENT 'System unique order ID',
  `listing_id` BIGINT DEFAULT NULL COMMENT 'On-chain Marketplace listingId',
  `seller` VARCHAR(64) NOT NULL COMMENT 'Seller address',
  `buyer` VARCHAR(64) DEFAULT NULL COMMENT 'Buyer address',
  `nft_name` VARCHAR(255) DEFAULT NULL COMMENT 'Human readable NFT name',
  `nft_address` VARCHAR(64) NOT NULL COMMENT 'NFT contract address',
  `token_id` BIGINT NOT NULL COMMENT 'NFT tokenId',
  `amount` BIGINT NOT NULL COMMENT 'Amount (ERC1155)',
  `price` DECIMAL(36,0) NOT NULL COMMENT 'Price in wei',
  `status` VARCHAR(20) NOT NULL COMMENT 'INIT, LISTED, LOCKED, SETTLING, SUCCESS, FAILED, CANCELED',
  `tx_hash` VARCHAR(100) DEFAULT NULL COMMENT 'On-chain transaction hash',
  `deleted` TINYINT NOT NULL DEFAULT 0 COMMENT 'Logical delete flag, 0=normal, 1=deleted',
  `created_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP COMMENT 'Create time',
  `updated_at` DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP COMMENT 'Update time',
  PRIMARY KEY (`order_id`),
  UNIQUE KEY `uk_orders_listing_id` (`listing_id`),
  UNIQUE KEY `uk_orders_tx_hash` (`tx_hash`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='NFT trading orders';
