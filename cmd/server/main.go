package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"gopkg.in/yaml.v3"

	"github.com/nft_market_go/internal/chain"
	"github.com/nft_market_go/internal/ipfs"
	"github.com/nft_market_go/internal/store"
)

// basicConfig holds minimal runtime configuration for the demo backend.
type basicConfig struct {
	RPCURL             string
	ChainID            int64
	MarketplaceAddress string
	ProjectNFTAddress  string
	Project1155Address string
	MySQLDSN           string
	PinataAPIURL       string
	PinataGatewayURL   string
	PinataAPIKey       string
	PinataSecretAPIKey string
	HTTPAddr           string
}

type yamlConfig struct {
	Blockchain struct {
		RPCURL  string `yaml:"rpc-url"`
		ChainID int64  `yaml:"chain-id"`
	} `yaml:"blockchain"`
	BSC struct {
		RPCURL string `yaml:"rpc-url"`
	} `yaml:"bsc"`
	Contracts struct {
		Marketplace string `yaml:"marketplace"`
		ProjectNFT  string `yaml:"project-nft"`
		Project1155 string `yaml:"project-1155"`
	} `yaml:"contracts"`
	MySQL struct {
		DSN string `yaml:"dsn"`
	} `yaml:"mysql"`
	IPFS struct {
		APIURL       string `yaml:"api-url"`
		GatewayURL   string `yaml:"gateway-url"`
		APIKey       string `yaml:"api-key"`
		SecretAPIKey string `yaml:"secret-api-key"`
	} `yaml:"ipfs"`
	Server struct {
		Addr string `yaml:"addr"`
	} `yaml:"server"`
}

// loadConfig reads config from config.yaml (if present) and environment variables.
// Environment variables override YAML values when both are set.
func loadConfig() (*basicConfig, error) {
	cfg := &basicConfig{}

	// 1) Load from config.yaml if it exists.
	if data, err := os.ReadFile("config.yaml"); err == nil {
		var yc yamlConfig
		if err := yaml.Unmarshal(data, &yc); err != nil {
			return nil, err
		}

		// Prefer the more generic blockchain section if present, fall back to legacy bsc.
		if yc.Blockchain.RPCURL != "" {
			cfg.RPCURL = yc.Blockchain.RPCURL
		} else {
			cfg.RPCURL = yc.BSC.RPCURL
		}
		cfg.ChainID = yc.Blockchain.ChainID
		cfg.MarketplaceAddress = yc.Contracts.Marketplace
		cfg.ProjectNFTAddress = yc.Contracts.ProjectNFT
		cfg.Project1155Address = yc.Contracts.Project1155
		cfg.MySQLDSN = yc.MySQL.DSN
		cfg.PinataAPIURL = yc.IPFS.APIURL
		cfg.PinataGatewayURL = yc.IPFS.GatewayURL
		cfg.PinataAPIKey = yc.IPFS.APIKey
		cfg.PinataSecretAPIKey = yc.IPFS.SecretAPIKey
		cfg.HTTPAddr = yc.Server.Addr
	}

	// 2) Override with environment variables when set.
	if v := os.Getenv("BSC_TESTNET_RPC_URL"); v != "" {
		cfg.RPCURL = v
	}
	if v := os.Getenv("NFT_MARKETPLACE_ADDRESS"); v != "" {
		cfg.MarketplaceAddress = v
	}
	if v := os.Getenv("PROJECT_NFT_ADDRESS"); v != "" {
		cfg.ProjectNFTAddress = v
	}
	if v := os.Getenv("PROJECT_1155_ADDRESS"); v != "" {
		cfg.Project1155Address = v
	}
	if v := os.Getenv("MYSQL_DSN"); v != "" {
		cfg.MySQLDSN = v
	}
	if v := os.Getenv("PINATA_API_URL"); v != "" {
		cfg.PinataAPIURL = v
	}
	if v := os.Getenv("PINATA_GATEWAY_URL"); v != "" {
		cfg.PinataGatewayURL = v
	}
	if v := os.Getenv("PINATA_API_KEY"); v != "" {
		cfg.PinataAPIKey = v
	}
	if v := os.Getenv("PINATA_SECRET_API_KEY"); v != "" {
		cfg.PinataSecretAPIKey = v
	}
	if v := os.Getenv("HTTP_ADDR"); v != "" {
		cfg.HTTPAddr = v
	}

	if cfg.RPCURL == "" {
		return nil, ErrMissingRPCURL
	}

	return cfg, nil
}

// ErrMissingRPCURL is returned when RPC URL is not configured.
var ErrMissingRPCURL = &configError{"BSC_TESTNET_RPC_URL is required"}

type configError struct {
	msg string
}

func (e *configError) Error() string {
	return e.msg
}

// swaggerJSON is a minimal Swagger 2.0 spec describing the RESTful APIs.
// It is served at /swagger/doc.json.
const swaggerJSON = `{
  "swagger": "2.0",
  "info": {
    "title": "NFT Market Go API",
    "version": "1.0.0",
    "description": "Gin + MySQL backend for NFT marketplace demo, including IPFS (Pinata) image upload."
  },
  "basePath": "/",
  "schemes": ["http"],
  "paths": {
    "/health": {
      "get": {
        "summary": "Health check",
        "produces": ["application/json"],
        "responses": {
          "200": {
            "description": "OK"
          }
        }
      }
    },
    "/api/v1/assets": {
      "get": {
        "summary": "List NFT assets by owner",
        "parameters": [
          {
            "name": "owner",
            "in": "query",
            "required": true,
            "type": "string",
            "description": "Owner wallet address"
          }
        ],
        "responses": {
          "200": {
            "description": "OK"
          }
        }
      },
      "post": {
        "summary": "Upload image to IPFS and create NFT asset metadata",
        "consumes": ["multipart/form-data"],
        "parameters": [
          {
            "name": "owner",
            "in": "formData",
            "required": true,
            "type": "string"
          },
          {
            "name": "name",
            "in": "formData",
            "required": true,
            "type": "string"
          },
          {
            "name": "file",
            "in": "formData",
            "required": true,
            "type": "file"
          }
        ],
        "responses": {
          "200": {
            "description": "OK"
          }
        }
      }
    },
    "/api/v1/assets/{id}": {
      "get": {
        "summary": "Get NFT asset by ID",
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "type": "integer",
            "format": "int64"
          }
        ],
        "responses": {
          "200": {
            "description": "OK"
          },
          "404": {
            "description": "Not found"
          }
        }
      }
    },
    "/api/v1/assets/{id}/mint-info": {
      "post": {
        "summary": "Update minted NFT info (token_id, nft_address, amount) for an asset",
        "consumes": ["application/json"],
        "parameters": [
          {
            "name": "id",
            "in": "path",
            "required": true,
            "type": "integer",
            "format": "int64"
          },
          {
            "name": "body",
            "in": "body",
            "required": true,
            "schema": {
              "type": "object",
              "properties": {
                "token_id": {
                  "type": "integer",
                  "format": "int64"
                },
                "nft_address": {
                  "type": "string"
                },
                "amount": {
                  "type": "integer",
                  "format": "int64"
                }
              },
              "required": ["token_id", "nft_address"]
            }
          }
        ],
        "responses": {
          "200": {
            "description": "OK"
          }
        }
      }
    },
    "/api/v1/assets/by-nft": {
      "get": {
        "summary": "Get NFT asset by on-chain nft_address and token_id",
        "parameters": [
          {
            "name": "nft_address",
            "in": "query",
            "required": true,
            "type": "string"
          },
          {
            "name": "token_id",
            "in": "query",
            "required": true,
            "type": "integer",
            "format": "int64"
          }
        ],
        "responses": {
          "200": {
            "description": "OK"
          },
          "404": {
            "description": "Not found"
          }
        }
      }
    },
    "/api/v1/orders": {
      "get": {
        "summary": "List recent marketplace orders",
        "responses": {
          "200": {
            "description": "OK"
          }
        }
      }
    },
    "/api/v1/orders/{listingId}": {
      "get": {
        "summary": "Get marketplace order by listingId",
        "parameters": [
          {
            "name": "listingId",
            "in": "path",
            "required": true,
            "type": "integer",
            "format": "int64"
          }
        ],
        "responses": {
          "200": {
            "description": "OK"
          },
          "404": {
            "description": "Not found"
          }
        }
      }
    }
  }
}`

// swaggerHTML renders Swagger UI from a CDN and loads our /swagger/doc.json.
const swaggerHTML = `<!DOCTYPE html>
<html>
  <head>
    <meta charset="utf-8">
    <title>NFT Market Go API Docs</title>
    <link rel="stylesheet" type="text/css" href="https://unpkg.com/swagger-ui-dist/swagger-ui.css" />
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist/swagger-ui-bundle.js"></script>
    <script>
      window.addEventListener('load', function() {
        const ui = SwaggerUIBundle({
          url: '/swagger/doc.json',
          dom_id: '#swagger-ui'
        });
        window.ui = ui;
      });
    </script>
  </body>
</html>`

func main() {
	cfg, err := loadConfig()
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	if cfg.MySQLDSN == "" {
		log.Fatal("MYSQL_DSN is required, example: user:password@tcp(127.0.0.1:3306)/nft_market?parseTime=true&charset=utf8mb4")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Establish a basic connection to BSC testnet (or other EVM RPC).
	ethClient, err := ethclient.DialContext(ctx, cfg.RPCURL)
	if err != nil {
		log.Fatalf("failed to connect to rpc: %v", err)
	}
	defer ethClient.Close()

	// Connect to MySQL using database/sql.
	db, err := sql.Open("mysql", cfg.MySQLDSN)
	if err != nil {
		log.Fatalf("failed to open mysql: %v", err)
	}
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("failed to ping mysql: %v", err)
	}
	defer db.Close()

	log.Printf("connected to mysql")

	// Initialize basic schema for marketplace orders and NFT assets.
	orderStore := store.NewOrderStore(db)
	if err := orderStore.InitSchema(ctx); err != nil {
		log.Fatalf("failed to init orders schema: %v", err)
	}

	assetStore := store.NewNftAssetStore(db)
	if err := assetStore.InitSchema(ctx); err != nil {
		log.Fatalf("failed to init nft_assets schema: %v", err)
	}

	// IPFS (Pinata) client for uploading files.
	ipfsClient := ipfs.NewPinataClient(
		cfg.PinataAPIURL,
		cfg.PinataGatewayURL,
		cfg.PinataAPIKey,
		cfg.PinataSecretAPIKey,
	)

	// Start marketplace event scanner (Listed / Cancelled / Sold) to sync orders table.
	if cfg.MarketplaceAddress == "" {
		log.Printf("NFT_MARKETPLACE_ADDRESS not set, marketplace scanner disabled")
	} else {
		marketAddr := common.HexToAddress(cfg.MarketplaceAddress)
		scanner, err := chain.NewMarketplaceScanner(ethClient, marketAddr, orderStore, log.Default())
		if err != nil {
			log.Printf("failed to init marketplace scanner: %v", err)
		} else {
			go scanner.Run(context.Background())
			log.Printf("marketplace scanner started for contract %s", cfg.MarketplaceAddress)
		}
	}

	log.Printf("connected to rpc %s", cfg.RPCURL)

	// TODO: in later steps, initialize contract bindings, event subscribers,
	// and services that expose marketplace/NFT read APIs.

	router := gin.Default()

	// Simple health check.
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// RESTful API v1.
	api := router.Group("/api/v1")

	// Orders (read-only, from MySQL mirror of on-chain marketplace).
	api.GET("/orders", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		orders, err := orderStore.ListRecent(ctx, 50)
		if err != nil {
			log.Printf("ListRecent error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.JSON(http.StatusOK, orders)
	})

	api.GET("/orders/:listingId", func(c *gin.Context) {
		idStr := c.Param("listingId")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid listingId"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		order, err := orderStore.GetByID(ctx, id)
		if err != nil {
			log.Printf("GetByID error: %v", err)
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			}
			return
		}

		c.JSON(http.StatusOK, order)
	})

	// NFT assets: upload to IPFS + metadata CRUD.
	api.POST("/assets", func(c *gin.Context) {
		owner := c.PostForm("owner")
		name := c.PostForm("name")

		if owner == "" || name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "owner and name are required"})
			return
		}

		fileHeader, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
			return
		}

		f, err := fileHeader.Open()
		if err != nil {
			log.Printf("open uploaded file error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to read file"})
			return
		}
		defer f.Close()

		ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
		defer cancel()

		uploadRes, err := ipfsClient.UploadFile(ctx, fileHeader.Filename, f)
		if err != nil {
			log.Printf("ipfs upload error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "ipfs upload failed"})
			return
		}

		asset := &store.NftAsset{
			Name:    name,
			Owner:   owner,
			CID:     uploadRes.CID,
			URL:     uploadRes.URL,
			TokenID: 0,
			Amount:  0,
			Deleted: 0,
		}

		id, err := assetStore.Insert(ctx, asset)
		if err != nil {
			log.Printf("insert asset error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db insert failed"})
			return
		}
		asset.ID = id

		c.JSON(http.StatusOK, asset)
	})

	// Update minted NFT info (token_id, nft_address, amount) after on-chain mint.
	api.POST("/assets/:id/mint-info", func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}

		var req struct {
			TokenID    int64  `json:"token_id"`
			NFTAddress string `json:"nft_address"`
			Amount     int64  `json:"amount"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid json body"})
			return
		}
		if req.TokenID <= 0 || req.NFTAddress == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "token_id and nft_address are required"})
			return
		}
		if req.Amount <= 0 {
			req.Amount = 1
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
		defer cancel()

		if err := assetStore.UpdateMintInfo(ctx, id, req.TokenID, req.NFTAddress, req.Amount); err != nil {
			log.Printf("UpdateMintInfo error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db update failed"})
			return
		}

		asset, err := assetStore.GetByID(ctx, id)
		if err != nil {
			log.Printf("GetByID after UpdateMintInfo error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "db query failed"})
			return
		}

		c.JSON(http.StatusOK, asset)
	})

	// Get asset by on-chain NFT (nft_address + token_id).
	api.GET("/assets/by-nft", func(c *gin.Context) {
		nftAddr := c.Query("nft_address")
		tokenIDStr := c.Query("token_id")
		if nftAddr == "" || tokenIDStr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "nft_address and token_id are required"})
			return
		}
		tokenID, err := strconv.ParseInt(tokenIDStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid token_id"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		asset, err := assetStore.GetByNFT(ctx, nftAddr, tokenID)
		if err != nil {
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			} else {
				log.Printf("GetByNFT error: %v", err)
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			}
			return
		}

		c.JSON(http.StatusOK, asset)
	})

	api.GET("/assets/:id", func(c *gin.Context) {
		idStr := c.Param("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid id"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		asset, err := assetStore.GetByID(ctx, id)
		if err != nil {
			log.Printf("GetByID asset error: %v", err)
			if err == sql.ErrNoRows {
				c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			}
			return
		}

		c.JSON(http.StatusOK, asset)
	})

	api.GET("/assets", func(c *gin.Context) {
		owner := c.Query("owner")
		if owner == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "owner query param is required"})
			return
		}

		ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
		defer cancel()

		assets, err := assetStore.ListByOwner(ctx, owner, 50)
		if err != nil {
			log.Printf("ListByOwner error: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.JSON(http.StatusOK, assets)
	})

	// Swagger: serve a minimal Swagger UI page backed by a static JSON spec.
	router.GET("/swagger/doc.json", func(c *gin.Context) {
		c.Data(http.StatusOK, "application/json; charset=utf-8", []byte(swaggerJSON))
	})

	router.GET("/swagger", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, swaggerHTML)
	})
	router.GET("/swagger/index.html", func(c *gin.Context) {
		c.Header("Content-Type", "text/html; charset=utf-8")
		c.String(http.StatusOK, swaggerHTML)
	})

	addr := cfg.HTTPAddr
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("starting http server on %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("http server error: %v", err)
	}
}
