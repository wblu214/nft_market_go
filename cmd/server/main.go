package main

import (
	"context"
	"database/sql"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"

	"github.com/nft_market_go/internal/ipfs"
	"github.com/nft_market_go/internal/store"
)

// basicConfig holds minimal runtime configuration for the demo backend.
type basicConfig struct {
	RPCURL              string
	MarketplaceAddress  string
	ProjectNFTAddress   string
	Project1155Address  string
	MySQLDSN            string
	PinataAPIURL        string
	PinataGatewayURL    string
	PinataAPIKey        string
	PinataSecretAPIKey  string
}

// loadConfig reads config from environment variables.
func loadConfig() (*basicConfig, error) {
	cfg := &basicConfig{
		RPCURL:             os.Getenv("BSC_TESTNET_RPC_URL"),
		MarketplaceAddress: os.Getenv("NFT_MARKETPLACE_ADDRESS"),
		ProjectNFTAddress:  os.Getenv("PROJECT_NFT_ADDRESS"),
		Project1155Address: os.Getenv("PROJECT_1155_ADDRESS"),
		MySQLDSN:           os.Getenv("MYSQL_DSN"),
		PinataAPIURL:       os.Getenv("PINATA_API_URL"),
		PinataGatewayURL:   os.Getenv("PINATA_GATEWAY_URL"),
		PinataAPIKey:       os.Getenv("PINATA_API_KEY"),
		PinataSecretAPIKey: os.Getenv("PINATA_SECRET_API_KEY"),
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

	addr := ":8080"
	log.Printf("starting http server on %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatalf("http server error: %v", err)
	}
}
