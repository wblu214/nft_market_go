# NFT Market Go 项目结构说明

本文从整体结构、各层职责和核心业务流程三个角度拆解项目，方便快速上手和排查问题。

---

## 1. 项目整体概览

- **定位**：一个最小可用的 NFT 市场后端 Demo，技术栈：
  - Go + Gin（HTTP API）
  - MySQL（订单与素材数据）
  - Redis（并发控制 / 分布式锁）
  - BSC Testnet（链上 Marketplace 合约）
  - IPFS（Pinata，图片存储）
- **核心能力**：
  - 用户上传图片到 IPFS，生成 NFT 素材（`nft_assets` 表）
  - 与链上合约配合完成：挂单、成交、取消，对应的订单镜像（`orders` 表）
  - 通过 Redis + MySQL 事务 + 链上事件扫描 & 定时对账，保证同一 NFT / 同一挂单在高并发场景下的最终一致性

---

## 2. 目录结构总览

```text
.
├── cmd/
│   └── server/          # 可执行程序入口，HTTP API、依赖注入、生命周期管理
├── internal/
│   ├── store/           # MySQL 数据访问层（DAO），封装订单和 NFT 素材表
│   ├── chain/           # 链上 Marketplace 扫描与对账逻辑
│   ├── ipfs/            # Pinata 客户端，负责文件上传到 IPFS
│   └── lock/            # 基于 Redis 的分布式锁封装
├── sql/                 # 数据库建表 SQL（DDL）
├── docs/                # 合约 ABI 与 API 文档、项目结构文档
├── config.yaml          # 本地运行示例配置（RPC / MySQL / Redis / IPFS / HTTP）
├── go.mod, go.sum       # Go 依赖管理
└── .gitignore           # 忽略 config.yaml、本地 go build 缓存
```

---

## 3. 各目录职责与关键文件

### 3.1 `cmd/server/` —— 应用入口 & HTTP API

**`cmd/server/main.go`**

- 负责所有依赖的初始化与注入：
  - 从 `config.yaml` / 环境变量加载配置（`loadConfig`）
  - 初始化：
    - `ethclient.Client`（BSC RPC）
    - `*sql.DB`（MySQL）
    - `*redis.Client`（Redis）
    - `store.OrderStore` / `store.NftAssetStore`
    - `ipfs.PinataClient`
    - `chain.MarketplaceScanner`
  - 调用 `InitSchema`，确保必要表存在：
    - `orders`：`sql/create_orders_table.sql`
    - `nft_assets`：`sql/create_nft_assets_table.sql`
- 暴露 HTTP API（基于 Gin）：
  - `GET  /health`
  - 订单相关：
    - `GET  /api/v1/orders`：最近订单列表
    - `GET  /api/v1/orders/:listingId`：按 **listingId** 查订单
    - `POST /api/v1/orders`：挂单（创建 / 更新订单 + 逻辑删除对应素材）
    - `POST /api/v1/orders/:listingId/status`：更新订单状态（成交 / 取消），并同步素材归属
  - NFT 素材相关：
    - `POST /api/v1/assets`：上传图片到 Pinata，创建素材记录
    - `POST /api/v1/assets/:id/mint-info`：上链 mint 完成后，写回 `token_id` / `nft_address` / `amount`
    - `GET  /api/v1/assets/by-nft`：按 `(nft_address, token_id)` 查询素材
    - `GET  /api/v1/assets/:id`：按主键 ID 查询素材
    - `GET  /api/v1/assets?owner=...`：按 owner 地址列出素材
- 并发 & 一致性关键点（都在 `main.go` 中）：
  - 通过 `lock.NewRedisLocker` + `orderLocker.Acquire(...)`：
    - 对 `POST /orders`、`POST /orders/:listingId/status` 按 **listingId** 上 Redis 锁
  - 对关键写操作使用显式 `db.BeginTx`：
    - 订单写入使用 `OrderStore.UpsertTx`
    - 资产更新使用 `NftAssetStore.SoftDeleteByNFTTx / RestoreByNFTTx / UpdateOwnerByNFTTx`
  - 对状态更新接口使用：
    - `OrderStore.GetByIDForUpdateTx(... FOR UPDATE)` 锁订单行
    - 简单状态机约束：订单一旦处于 `SUCCESS` / `CANCELED`，禁止切换到另一种终态（防止“双花”）

### 3.2 `internal/store/` —— MySQL 访问层

**`internal/store/order_store.go`**

- 定义 `OrderStatus` 枚举 & `Order` 结构体（对应 `orders` 表）。
- `OrderStore` 封装对 `orders` 表的所有读写：
  - `InitSchema`：执行 `sql/create_orders_table.sql`
  - `Upsert` / `UpsertTx`：
    - 使用 `INSERT ... ON DUPLICATE KEY UPDATE`
    - 以 `listing_id` 作为唯一键实现幂等写入
  - `GetByID`：
    - 按 `listing_id` 查询单条订单，做了 NULL -> 默认值的处理
  - `GetByIDForUpdateTx`：
    - 同 `GetByID`，但在事务内附加 `FOR UPDATE` 锁行，用于状态更新接口
  - `ListRecent`：
    - 按 `updated_at` 倒序、`deleted = 0`，列出最近 N 条订单

**`internal/store/nft_asset_store.go`**

- 定义 `NftAsset` 结构体（对应 `nft_assets` 表）。
- `NftAssetStore` 封装 NFT 素材相关操作：
  - `InitSchema`：执行 `sql/create_nft_assets_table.sql`
  - `Insert`：插入新素材，并返回自增 `id`
  - `GetByID` / `ListByOwner` / `ExistsByOwnerAndURL`
  - `SoftDeleteByNFT` / `RestoreByNFT` / `UpdateOwnerByNFT`
    - 以及对应的 Tx 版本：`SoftDeleteByNFTTx` / `RestoreByNFTTx` / `UpdateOwnerByNFTTx`
    - 用于在一个事务里与订单操作一起提交 / 回滚
  - `UpdateMintInfo`：上链后补写 `token_id` / `nft_address` / `amount`
  - `GetByNFT`：按 `(nft_address, token_id)` 定位一条素材

**`internal/store/sql_exec.go`**

- 抽象 `sqlExecutor` 接口，让 `*sql.DB` 与 `*sql.Tx` 共享同一套查询 / 执行逻辑：
  - 方便 `Upsert` / `SoftDeleteByNFT` 等方法同时在“无事务”和“显式事务”场景复用。

### 3.3 `internal/chain/` —— 链上 Marketplace 事件同步 & 对账

**`internal/chain/marketplace_scanner.go`**

- 使用 `docs/NFTMarketplace.abi.json` 解析 Marketplace 合约 ABI。
- 通过 `ethclient.Client` 扫描合约日志事件：
  - `Listed(listingId, seller, nft, tokenId, amount, price)`
  - `Cancelled(listingId)`
  - `Sold(listingId, buyer)`
- 核心能力：
  - `Run(ctx)`：
    - 从当前区块高度开始，每隔 `pollInterval`（5s）轮询：
      - 以 `maxBatchBlocks` 小批量调用 `FilterLogs`，避免 RPC 限流
      - 只筛选 3 个事件（Listed/Cancelled/Sold）
      - 对每条 log 调用 `handleListed / handleCancelled / handleSold`，将链上事件写入 `orders` 表
  - `ResyncRecent(ctx, lookbackBlocks)`：
    - 对最近 N 个区块重新调一次 `FilterLogs`，重新执行 `handleXXX`，用于“定时对账、修复遗漏事件”
- 事件处理细节：
  - `handleListed`：
    - 创建 / 更新订单，状态置为 `LISTED`，保存价格、TokenId、NFT 合约地址等信息
  - `handleCancelled`：
    - 若已有订单：更新状态为 `CANCELED`
    - 若没有：创建一个最小记录（只包含 listingId + 状态 + TxHash）
  - `handleSold`：
    - 若已有订单：写入 `buyer` 地址、状态 `SUCCESS`
    - 若没有：同样创建最小记录，填入 buyer + 状态 + TxHash

> 注意：Scanner 只管 **orders 表** 的镜像更新，`nft_assets` 的归属变化由 HTTP 层在状态接口中负责（事务内完成）。

### 3.4 `internal/ipfs/` —— Pinata 客户端

**`internal/ipfs/pinata_client.go`**

- `PinataClient` 封装 Pinata HTTP API：
  - 构造 multi-part 请求调用 `/pinning/pinFileToIPFS`
  - 解析返回的 `IpfsHash`，组合出 Gateway URL
- 被 `POST /api/v1/assets` 使用，用于上传前端图片文件到 IPFS，返回：
  - `CID`（内容地址）
  - `URL`（通过网关访问的公开 URL）

### 3.5 `internal/lock/` —— Redis 分布式锁

**`internal/lock/redis_lock.go`**

- `RedisLocker`：
  - 内部持有 `*redis.Client`，配置 key 前缀（项目中为 `nft_market:order:`）
  - `Acquire(ctx, key, ttl)`：
    - 基于 `SETNX + EX`，为指定 key 设置一个随机 value（防止误删他人锁）
    - 成功返回 `RedisLock`；失败返回 `ErrLockNotAcquired`
- `RedisLock`：
  - `Release(ctx)`：
    - 使用 Lua 脚本：先比对 value，再决定是否 `DEL`，保证释放锁安全
- 本项目中只在订单相关接口使用：
  - `POST /orders`
  - `POST /orders/:listingId/status`

---

## 4. SQL 与数据模型

### 4.1 `sql/create_orders_table.sql`

- 表：`orders`
- 关键字段：
  - `order_id`：自增主键（系统内部 ID）
  - `listing_id`：链上 Marketplace 的 `listingId`（唯一键 `uk_orders_listing_id`）
  - `seller` / `buyer`：卖家 & 买家地址
  - `nft_name` / `nft_address` / `token_id` / `amount` / `url`
  - `price`：`DECIMAL(36,0)`，以 wei 为单位
  - `status`：字符串，枚举值由 `OrderStatus` 定义
  - `tx_hash`：链上交易哈希（唯一键 `uk_orders_tx_hash`）
  - `deleted`：逻辑删除标记

### 4.2 `sql/create_nft_assets_table.sql`

- 表：`nft_assets`
- 关键字段：
  - `id`：自增主键
  - `name`：NFT 展示名
  - `owner`：当前持有者地址
  - `cid` / `url`：IPFS CID 与网关地址
  - `token_id` / `nft_address` / `amount`：上链后的 token 信息（可为 NULL）
  - `deleted`：逻辑删除标记（挂单时会临时置 1，避免被当作“可用素材”再挂一次）

---

## 5. 并发控制与最终一致性（整体视角）

1. **Redis 层（快速互斥）**
   - 对同一 `listingId` 的挂单、状态修改加分布式锁，避免多个请求同时操作同一订单。
2. **MySQL 事务层（强一致）**
   - 订单和素材的更新在单个事务中完成，错误则整体回滚：
     - 创建挂单：`orders` + `nft_assets.deleted=1`
     - 成交/取消：`orders.status` + `nft_assets` 恢复 / 改 owner
   - 状态更新使用 `SELECT ... FOR UPDATE` 锁行 + 终态检查，避免重复成交 / 反向状态切换。
3. **链上层（最终确权）**
   - 实时 scanner：轮询链上事件，尽量保持 DB 与链上同步。
   - 定时 `ResyncRecent`：定期重扫最近 N 个区块，即便实时阶段漏掉一些事件，也能最终修正 `orders`。

---

## 6. 启动与配置

### 6.1 配置文件 `config.yaml`

- `blockchain.rpc-url` / `chain-id`：BSC Testnet RPC 与链 ID
- `contracts.*`：Marketplace / 项目 NFT / 1155 合约地址
- `mysql.dsn`：MySQL 连接串（已带 `parseTime=true`、`charset=utf8mb4` 等参数）
- `redis.{addr,password,db}`：Redis 连接配置
- `ipfs.*`：Pinata API 地址、网关、Key/Secret
- `server.addr`：HTTP 监听地址（默认 `:8080`）

> 所有配置都可以通过环境变量覆盖（如 `BSC_TESTNET_RPC_URL`、`MYSQL_DSN`、`REDIS_ADDR` 等），适合部署时使用。

### 6.2 运行路径

1. 创建并初始化 MySQL / Redis 实例。
2. 按需修改 `config.yaml` 或设置环境变量。
3. 在项目根目录执行：
   ```bash
   go run ./cmd/server
   ```
4. 通过 `GET /health` 验证服务是否正常；再按文档调用资产和订单相关接口。

---

如果你希望，我可以在这个文档基础上再补一页“典型调用链示例”（比如：上传图片 → mint NFT → 挂单 → 成交），用时序图形式重新梳理一遍。**

