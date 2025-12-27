# NFT Demo 前端调用 API 文档（Gin 后端）

本文档描述前端需要调用的 HTTP REST 接口（Gin + MySQL + IPFS），用于配合链上合约（NFTMarketplace / ProjectNFT / Project1155）完成业务。

> 基础说明：
> - Base URL：`http://localhost:8080`（或你在 `config.yaml -> server.addr` 配置的地址）
> - 统一前缀：`/api/v1`
> - 返回格式：JSON
> - 未做鉴权（Demo 项目），前端只需直接发请求即可。

---

## 1. 健康检查

### 1.1 `GET /health`

- 作用：检查后端服务是否正常。
- 请求：无参数。
- 响应示例：

```json
{
  "status": "ok"
}
```

前端可以在启动时轮询一次，确认服务可用。

---

## 2. NFT 素材上传与查询（IPFS + MySQL）

后端负责把图片上传到 Pinata（IPFS），并在 MySQL 的 `nft_assets` 表里记录一条元数据。  
链上 mint（调用 `ProjectNFT` / `Project1155` 合约）由前端钱包完成，这部分见 `Backend_API_Doc.md`。

### 2.1 上传图片并生成 NFT 素材记录

`POST /api/v1/assets`

- 功能：前端上传图片 → 后端上传到 IPFS（Pinata） → 写入 `nft_assets` 表 → 返回素材信息。
- Content-Type：`multipart/form-data`
- 请求参数：
  - `owner`（form-data, string, 必填）：用户钱包地址，如 `0x1234...abcd`
  - `name`（form-data, string, 必填）：NFT 展示名称
  - `file`（form-data, file, 必填）：要上传的图片文件（png/jpg/gif 等）
- 响应示例：

```json
{
  "id": 1,
  "name": "My First NFT",
  "owner": "0x1234567890abcdef1234567890abcdef12345678",
  "cid": "Qm...",
  "url": "https://gateway.pinata.cloud/ipfs/Qm...",
  "token_id": 0,
  "nft_address": "",
  "amount": 0,
  "deleted": 0,
  "created_at": "2025-12-27T15:40:00Z",
  "updated_at": "2025-12-27T15:40:00Z"
}
```

- 前端使用建议：
  1. 用户选图 → 前端调用该接口，拿到 `cid / url`。
  2. 将 `url` 作为元数据里的 image 字段，组装完整 metadata（如 IPFS JSON），再调用链上 `ProjectNFT.mint(to, uri)` / `Project1155.mint(...)`。
  3. 链上 mint 成功后，调用 2.2 所述接口，把 `token_id` / `nft_address` / `amount` 回传给后端。

---

### 2.2 链上 mint 之后回传 tokenId / nftAddress / amount

`POST /api/v1/assets/{id}/mint-info`

- 功能：链上 mint 成功后，前端把生成的 `tokenId`、`nftAddress`、`amount` 回传给后端，补全 `nft_assets` 中的字段。
- 路径参数：
  - `id`（int64, 必填）：`nft_assets.id`，即第 2.1 步上传图片时返回的 `id`。
- Body（JSON）：

```json
{
  "token_id": 1,
  "nft_address": "0xaa6a15D595bA8F69680465FBE61d9d886057Cb1E",
  "amount": 1
}
```

- 说明：
  - ERC721：`amount` 固定为 `1`。
  - ERC1155：`amount` 为 mint 的份额数量。
- 响应：更新后的 asset 记录（与 2.1 返回结构一致）。

前端调用顺序建议：

1. `POST /api/v1/assets` 上传图片，拿到 `id` + `url`。
2. 调用链上 `mint`（钱包签名，得到 `tokenId`）。
3. 调用 `POST /api/v1/assets/{id}/mint-info`，把 `tokenId` / `nftAddress` / `amount` 回写后端。

之后就可以通过 `GET /api/v1/assets/by-nft` 或 `GET /api/v1/assets/:id` 查到完整信息。

---

### 2.3 按 ID 查询单个素材

`GET /api/v1/assets/:id`

- 功能：按主键 ID 查询一条 `nft_assets` 记录。
- 路径参数：
  - `id`（int64, 必填）：`nft_assets.id`
- 响应示例：

```json
{
  "id": 1,
  "name": "My First NFT",
  "owner": "0x1234567890abcdef1234567890abcdef12345678",
  "cid": "Qm...",
  "url": "https://gateway.pinata.cloud/ipfs/Qm...",
  "token_id": 0,
  "nft_address": "",
  "amount": 0,
  "deleted": 0,
  "created_at": "2025-12-27T15:40:00Z",
  "updated_at": "2025-12-27T15:40:00Z"
}
```

---

### 2.4 按 owner 查询素材列表

`GET /api/v1/assets?owner=0x...`

- 功能：查询某个地址名下的所有未删除素材（最多 50 条），用于“我的作品”、“我的素材库”页面。
- Query 参数：
  - `owner`（string, 必填）：钱包地址
- 响应示例（数组）：

```json
[
  {
    "id": 1,
    "name": "My First NFT",
    "owner": "0x1234567890abcdef1234567890abcdef12345678",
    "cid": "Qm...",
    "url": "https://gateway.pinata.cloud/ipfs/Qm...",
    "token_id": 0,
    "nft_address": "",
    "amount": 0,
    "deleted": 0,
    "created_at": "2025-12-27T15:40:00Z",
    "updated_at": "2025-12-27T15:40:00Z"
  }
]
```

---

### 2.5 用户点击 NFT 图片时，根据链上信息查询素材

`GET /api/v1/assets/by-nft?nft_address=...&token_id=...`

- 场景：用户在前端点击一个已经 mint 的 NFT（前端只知道链上信息：`nftAddress` + `tokenId`），需要从后端拿到当初上传图片时的 metadata（`url` / `name` / `owner` 等）。
- Query 参数：
  - `nft_address`（string, 必填）：NFT 合约地址（ERC721 或 ERC1155）
  - `token_id`（int64, 必填）：链上的 `tokenId`（或 ERC1155 的 `id`）
- 响应示例：

```json
{
  "id": 1,
  "name": "My First NFT",
  "owner": "0x1234567890abcdef1234567890abcdef12345678",
  "cid": "Qm...",
  "url": "https://gateway.pinata.cloud/ipfs/Qm...",
  "token_id": 1,
  "nft_address": "0xaa6a15D595bA8F69680465FBE61d9d886057Cb1E",
  "amount": 1,
  "deleted": 0,
  "created_at": "2025-12-27T15:40:00Z",
  "updated_at": "2025-12-27T15:40:00Z"
}
```

这样前端在任何“从链上拿到 NFT 信息”的场景下，只需要把 `nft_address` + `token_id` 传给后端，就能查回对应的图片和元数据。


## 3. 订单写入与查询

后端通过两种方式维护 `orders` 表：
- 订阅 `NFTMarketplace` 的事件 `Listed / Cancelled / Sold`（自动同步）；
- 前端在上架成功后主动回传一份订单信息（推荐，避免 RPC 不稳定）。

### 3.1 上架成功后回传订单信息（推荐）

`POST /api/v1/orders`

- 功能：前端在链上调用 `NFTMarketplace.list(...)` 成功后，把该笔订单的关键字段回传给后端，立即写入 `orders` 表。
- Body（JSON）：

```json
{
  "listing_id": 1001,
  "seller": "0xSeller...",
  "nft_address": "0xaa6a15D595bA8F69680465FBE61d9d886057Cb1E",
  "token_id": 1,
  "amount": 1,
  "price": "1000000000000000000",
  "tx_hash": "0x9f0593086fd71fafa7fa1f5f65921893ab4f8c8733b2f15ebe9529423d46b107"
}
```

- 字段说明：
  - `listing_id`：链上的 `listingId`（合约 `list` 的返回值或事件参数）
  - `seller`：当前钱包地址
  - `nft_address`：NFT 合约地址（ERC721 / ERC1155）
  - `token_id`：上架的 `tokenId`（或 ERC1155 的 `id`）
  - `amount`：数量（ERC721 = 1；ERC1155 为上架份数）
  - `price`：价格（wei，十进制字符串）
  - `tx_hash`：`list` 这笔交易的 hash（可选，但建议带上）

- 后端处理：
  - 写入 / 更新 `orders` 表，对应一条 `status = "LISTED"` 的订单；
  - 后续成交 / 撤单仍由链上事件将 `status` 更新为 `SUCCESS` / `CANCELED`。

- 前端调用时机：
  1. 钱包调用 `NFTMarketplace.list(...)`，等待交易确认；
  2. 拿到 `listingId`、`txHash`、`nft_address`、`token_id`、`amount`、`price`；
  3. 立刻调用 `POST /api/v1/orders` 把这些字段传给后端。

---

### 3.2 查询最近订单列表

`GET /api/v1/orders`

- 功能：返回最近更新的订单列表（最多 50 条），包含链上状态同步结果。
- 响应字段（简要说明，对应 `sql/create_orders_table.sql`）：
  - `order_id`：自增主键
  - `listing_id`：链上 `listingId`
  - `seller`：卖家地址
  - `buyer`：买家地址（未成交时为空）
  - `nft_name`：可选的 NFT 名称（目前为空，预留）
  - `nft_address`：NFT 合约地址（ERC721 或 ERC1155）
  - `token_id`：NFT 的 `tokenId`（或 ERC1155 的 `id`）
  - `amount`：数量（ERC721 固定 1，ERC1155 >= 1）
  - `price`：价格（wei，整数字符串）
  - `status`：订单状态：
    - `INIT`, `LISTED`, `LOCKED`, `SETTLING`, `SUCCESS`, `FAILED`, `CANCELED`
    - 合约事件目前只会驱动 `LISTED`、`SUCCESS`（Sold）、`CANCELED`
  - `tx_hash`：最近一次相关交易的 hash
  - `deleted`：逻辑删除标记（0 正常）
  - `created_at` / `updated_at`

示例响应：

```json
[
  {
    "order_id": 1,
    "listing_id": 1001,
    "seller": "0xSeller...",
    "buyer": "0xBuyer...",
    "nft_name": "",
    "nft_address": "0xaa6a15D595bA8F69680465FBE61d9d886057Cb1E",
    "token_id": 1,
    "amount": 1,
    "price": "1000000000000000000",
    "status": "SUCCESS",
    "tx_hash": "0x...",
    "deleted": 0,
    "created_at": "2025-12-27T15:45:00Z",
    "updated_at": "2025-12-27T15:50:00Z"
  }
]
```

前端使用建议：

- 市场首页 / 浏览列表：直接用这个接口获取最近的挂单、成交记录。
- 如果需要按 seller/buyer 过滤，可以在后端再加条件接口。

---

### 3.3 按 listingId 查询单个订单

`GET /api/v1/orders/:listingId`

- 功能：按链上 `listingId` 查询单个订单。
- 路径参数：
  - `listingId`（int64, 必填）：`NFTMarketplace` 合约生成的 `listingId`
- 响应示例：

```json
{
  "order_id": 1,
  "listing_id": 1001,
  "seller": "0xSeller...",
  "buyer": "0xBuyer...",
  "nft_name": "",
  "nft_address": "0xaa6a15D595bA8F69680465FBE61d9d886057Cb1E",
  "token_id": 1,
  "amount": 1,
  "price": "1000000000000000000",
  "status": "SUCCESS",
  "tx_hash": "0x...",
  "deleted": 0,
  "created_at": "2025-12-27T15:45:00Z",
  "updated_at": "2025-12-27T15:50:00Z"
}
```

前端使用建议：

- 订单详情页：通过路由里的 `listingId` 调用此接口，展示订单状态、价格、买卖双方等信息。

---

## 4. 错误返回约定

所有接口在出错时，统一返回类似结构：

```json
{
  "error": "错误提示文本"
}
```

常见场景：

- 400：参数不合法（如 `owner` / `name` / `file` 缺失等）
- 404：资源不存在（如订单不存在、素材 ID 不存在）
- 500：内部错误（数据库错误、IPFS 上传失败等）

前端可以统一判断 HTTP 状态码和 `error` 字段做提示。
