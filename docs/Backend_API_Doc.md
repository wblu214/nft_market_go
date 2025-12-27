# NFT 項目鏈上接口文檔（Go 後端 & 前端）

本文件是你這個 NFT Demo 項目的唯一後端對接文檔，涵蓋三個合約：

- `NFTMarketplace`（結算層）
- `ProjectNFT`（ERC721）
- `Project1155`（ERC1155）

並明確區分：哪些由前端用錢包簽名調用，哪些由 Go 後端讀取 / 監聽。

---

## 1. 網絡 & 合約地址

- 網絡：BSC Testnet  
- Chain ID：`97`  
- RPC：後端自行配置，例如 `BSC_TESTNET_RPC_URL`  
- 合約地址：
  - `NFTMarketplace`：`0xCAD727e729e6737405773B05D2dac105a3026764`
  - `ProjectNFT`（ERC721）：`0xaa6a15D595bA8F69680465FBE61d9d886057Cb1E`
  - `Project1155`（ERC1155）：`0x1fF53616471271d80E17BD2A46C863d3Fd38aE81`
- ABI 文件位置 在docs文件夹

Go 後端可以直接讀取這些 JSON 裡的 `abi` 字段，用 go-ethereum 的 `abi.JSON` 或 abigen 生成 Go 綁定。

---

## 2. Go 後端需要準備的配置

對於所有合約，Go 端除了合約地址之外，還需要：

- RPC 連接：
  - `client, err := ethclient.Dial(rpcURL)`
- Chain ID：
  - 用於創建寫交易的 `TransactOpts`（如果後端需要用平台錢包發交易），BSC Testnet 為 `97`。
- ABI：
  - 從 `out/*.json` 讀取 `abi` 字段。
- 可選：平台錢包私鑰（僅當需要後端主動發鏈上交易時）：
  - 用 `bind.NewKeyedTransactorWithChainID` 創建 `*bind.TransactOpts`。
  - 目前合約沒有 owner/管理員邏輯，是否使用平台錢包完全取決於你的業務。

大部分用戶相關操作（mint / approve / list / cancel / buy）建議都在前端通過用戶錢包簽名完成，後端只做只讀查詢和事件訂閱。

---

## 3. 合約接口 & 前端 / 後端分工

### 3.1 NFTMarketplace（結算層）

主要負責：掛單、取消、成交，檢查授權並完成 NFT 和 ETH 的原子交換。

**前端（用戶錢包簽名）**

- `list(address nft, uint256 tokenId, uint256 amount, uint256 price)`  
  - 調用方：賣家（必須是當前 NFT 持有者）。  
  - 用戶先在前端對 NFT 合約做授權，然後用錢包發交易調用 `list`。
- `cancel(uint256 listingId)`  
  - 調用方：該 listing 的 `seller`。  
  - 用戶用錢包撤單。
- `buy(uint256 listingId)` `payable`  
  - 調用方：買家。  
  - 前端錢包發送交易並附帶 `msg.value = price`。

**後端（Go，只讀 / 監聽）**

- 只讀函數：
  - `listings(uint256 listingId)`  
    - 查詢鏈上的掛單詳情：`seller, nft, tokenId, amount, price, active`。
  - `nextListingId()`  
    - 當前已使用的最大掛單 ID + 1。
- 事件（推薦後端訂閱）：
  - `Listed(listingId, seller, nft, tokenId, amount, price)`
  - `Cancelled(listingId)`
  - `Sold(listingId, buyer)`

後端可以通過 `FilterLogs` / `SubscribeFilterLogs` 監聽這三個事件，將鏈上狀態同步到自己的訂單數據表。

---

### 3.2 ProjectNFT（ERC721）

單份 NFT，每次 mint 生成一個新的 `tokenId`。

**前端（用戶錢包簽名）**

- `mint(address to, string uri)`  
  - 用戶創建自己的 ERC721 NFT。  
  - `to` 通常就是當前用戶地址，`uri` 為 IPFS / HTTP 元數據地址。
- `setApprovalForAll(address operator, bool approved)`  
  - 對 `NFTMarketplace` 做全局授權。  
  - `operator = NFTMarketplace` 地址。
- `approve(address to, uint256 tokenId)`（可選）  
  - 對單個 token 授權 Marketplace。
- `transferFrom` / `safeTransferFrom`  
  - 如果有需要讓用戶在錢包裡直接轉移 NFT，也應由前端發交易。

**後端（Go，只讀 / 監聽）**

- 只讀函數：
  - `ownerOf(uint256 tokenId)`
  - `balanceOf(address owner)`
  - `tokenURI(uint256 tokenId)`
  - `isApprovedForAll(address owner, address operator)`
  - `getApproved(uint256 tokenId)`
  - `name()`, `symbol()`, `nextTokenId()`, `supportsInterface(bytes4)`
- 事件（可選監聽）：
  - `Transfer`
  - `Approval`
  - `ApprovalForAll`

---

### 3.3 Project1155（ERC1155）

多份 NFT，同一個 `id` 可以有多個份額。

**前端（用戶錢包簽名）**

- `mint(address to, uint256 id, uint256 amount, string newUri)`  
  - 用戶為某個 `id` 鑄造若干份 ERC1155 代幣。  
  - 如果是該 `id` 的第一次 mint 且 `newUri` 非空，會記錄元數據 URI。
- `setApprovalForAll(address operator, bool approved)`  
  - 對 `NFTMarketplace` 做授權，允許其批量轉移自己的 ERC1155 資產。
- `safeTransferFrom` / `safeBatchTransferFrom`  
  - 用戶手動轉移 ERC1155 代幣（如非通過 Marketplace 的場景）。

**後端（Go，只讀 / 監聽）**

- 只讀函數：
  - `balanceOf(address account, uint256 id)`
  - `balanceOfBatch(address[] accounts, uint256[] ids)`
  - `isApprovedForAll(address account, address operator)`
  - `uri(uint256 id)`
  - `supportsInterface(bytes4)`
- 事件（可選監聽）：
  - `TransferSingle`
  - `TransferBatch`
  - `ApprovalForAll`
  - `URI`

---

## 4. 典型業務流程（前端 / 後端協作）

### 4.1 ERC721：用戶鑄造 -> 授權 -> 上架 -> 購買

1. 前端：用戶錢包調用 `ProjectNFT.mint(to, uri)`，獲得 `tokenId`。  
2. 前端：用戶錢包調用 `ProjectNFT.setApprovalForAll(NFTMarketplace, true)` 或 `approve(NFTMarketplace, tokenId)`。  
3. 前端：用戶錢包調用 `NFTMarketplace.list(ProjectNFT, tokenId, 1, price)`，完成上架。  
4. 後端（Go）：訂閱 `Listed` 事件，寫入/更新訂單狀態。  
5. 前端：其他用戶錢包調用 `NFTMarketplace.buy(listingId)` 並支付 `price`。  
6. 後端（Go）：訂閱 `Sold` 事件，更新訂單為已成交，同步 buyer、block 等信息。

### 4.2 ERC1155：用戶鑄造 -> 授權 -> 上架 -> 購買

1. 前端：用戶錢包調用 `Project1155.mint(to, id, amount, newUri)`。  
2. 前端：用戶錢包調用 `Project1155.setApprovalForAll(NFTMarketplace, true)`。  
3. 前端：用戶錢包調用 `NFTMarketplace.list(Project1155, id, amountForSale, price)`。  
4. 後端（Go）：訂閱 `Listed` 事件，寫入/更新訂單。  
5. 前端：其他用戶錢包調用 `NFTMarketplace.buy(listingId)` 並支付 `price`。  
6. 後端（Go）：訂閱 `Sold` 事件，更新訂單狀態、同步買家地址。

---

## 5. Go 端實現層面的提示（簡要）

- 推薦使用：
  - `github.com/ethereum/go-ethereum/ethclient`
  - `github.com/ethereum/go-ethereum/accounts/abi`
  - `github.com/ethereum/go-ethereum/accounts/abi/bind`
- 只讀調用：
  - 使用 `bind.CallOpts`（無需私鑰）。
- 事件訂閱：
  - 使用 `FilterLogs` 做批量同步；
  - 或 `SubscribeFilterLogs` 做實時監聽。
- 如需後端錢包發交易（可選）：
  - 使用 `bind.NewKeyedTransactorWithChainID` 創建 `TransactOpts`；
  - 使用 `BoundContract.Transact` / abigen 生成的綁定方法發送寫交易。

這一份文檔即覆蓋了原來兩份 `NFTMarketplace_API_for_Backend.md` 和 `ProjectTokens_API_for_Backend.md` 的內容，後續只需要維護本文件即可。

