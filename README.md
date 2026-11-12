# Grid KYC Verifier

A Go-based identity verification service that integrates with iDenfy to provide Know Your Customer (KYC) functionality. It verifies government-issued identity documents and ensures compliance requirements are met before users can deploy workloads.

## What this is

This service provides a RESTful API for identity verification workflows. It issues verification tokens, processes callbacks from the identity verification provider, and records verification status. The service uses challenge-response authentication tied to blockchain accounts, ensuring that only the rightful owner of an account can initiate verification for it.

## What this repository contains

- **API server** — Go HTTP server with RESTful endpoints for token issuance, status checks, and sponsorship management
- **iDenfy integration** — Webhook handlers and client for iDenfy verification flows
- **Blockchain client** — Ledger Chain integration for account and balance verification
- **Authentication layer** — Challenge-response middleware using sr25519/ed25519 signatures
- **Data persistence** — MongoDB repository layer for verification records and sponsorships
- **Swagger documentation** — Interactive API docs served at `/docs`
- **Docker Compose setup** — Containerized deployment configuration for the service and database

## Role in the stack

The KYC verifier acts as a gatekeeper in the deployment pipeline. Before a user can create deployments, their account must be verified through this service. It bridges external identity verification with on-chain account status, providing a compliance layer without exposing personal data to the chain.

## Relation to ThreeFold

This technology is used within the ThreeFold ecosystem and was first deployed on the ThreeFold Grid. The component itself is designed as reusable infrastructure technology and should be understood by its technical function first, independent of any specific deployment.

## Ownership

This repository is owned and maintained by TF-Tech NV, a Belgian company responsible for the development and maintenance of this technology.

## Features

- Identity verification using iDenfy
- Blockchain integration with Ledger Chain (Substrate-based)
- MongoDB for data persistence
- RESTful API endpoints for KYC operations
- Swagger documentation
- Containerized deployment
- Rate limiting per IP and per identity
- Sponsorship system for verified sponsors to vouch for sponsees

## Prerequisites

- Go 1.22+
- MongoDB 4.4+
- Docker and Docker Compose (for containerized deployment)
- iDenfy API credentials

## Installation

1. Clone the repository:

    ```bash
    git clone https://github.com/threefoldtech/tf-kyc-verifier.git
    cd grid_kyc_verifier
    ```

2. Set up your environment variables:

    ```bash
    cp .app.env.example .app.env
    cp .db.env.example .db.env
    cp .express.env.example .express.env # only if you are using mongo-express for development
    ```

Edit `.app.env` and `.db.env` with your specific configuration details.

## Configuration

The application uses environment variables for configuration. Here is a list of all available configuration options:

### Database configuration

- `MONGO_URI`: MongoDB connection URI (default: `mongodb://localhost:27017`)
- `DATABASE_NAME`: Name of the MongoDB database (default: `tf-kyc-db`)

### Server configuration

- `PORT`: Port on which the server will run (default: `8080`)

### iDenfy configuration

- `IDENFY_API_KEY`: API key for iDenfy service (required)
- `IDENFY_API_SECRET`: API secret for iDenfy service (required)
- `IDENFY_BASE_URL`: Base URL for iDenfy API (default: `https://ivs.idenfy.com`)
- `IDENFY_CALLBACK_SIGN_KEY`: Callback signing key for iDenfy webhooks (required, at least 32 characters)
- `IDENFY_WHITELISTED_IPS`: Comma-separated list of whitelisted IPs for iDenfy callbacks
- `IDENFY_DEV_MODE`: Enable development mode for iDenfy integration (default: `false`)
- `IDENFY_CALLBACK_URL`: URL for iDenfy verification update callbacks
- `IDENFY_NAMESPACE`: Namespace for isolating different KYC services' data within the same iDenfy backend (default: `""`)

### Ledger Chain configuration

- `TFCHAIN_WS_PROVIDER_URL`: WebSocket provider URL for Ledger Chain (default: `wss://tfchain.grid.tf`)

### Verification settings

- `VERIFICATION_SUSPICIOUS_VERIFICATION_OUTCOME`: Outcome for suspicious verifications (default: `APPROVED`)
- `VERIFICATION_EXPIRED_DOCUMENT_OUTCOME`: Outcome for expired documents (default: `REJECTED`)
- `VERIFICATION_MIN_BALANCE_TO_VERIFY_ACCOUNT`: Minimum balance in uTFT required to verify an account (default: `10000000`)
- `VERIFICATION_ALWAYS_VERIFIED_IDS`: Comma-separated list of Ledger Chain SS58 addresses that are always verified (default: `""`)
- `VERIFICATION_ALWAYS_VERIFIED_IDS_ONLY`: When true, creation of KYC tokens is disabled on this network (default: `false`)
- `VERIFICATION_DISABLED`: When true, KYC is switched off. `GET /api/v1/status` reports every client as verified, `POST /api/v1/token` answers `409 user already verified` without starting a provider session, `GET /api/v1/data` returns an empty record instead of stored data, and `POST /api/v1/sponsorships` answers `403`. Stored records are left in the database and are used again when the flag is turned off (default: `false`)

### Rate limiting

**IP-based:**
- `IP_LIMITER_MAX_TOKEN_REQUESTS`: Maximum number of token requests per IP (default: `4`)
- `IP_LIMITER_TOKEN_EXPIRATION`: Token expiration time in minutes (default: `1440`)

**ID-based:**
- `ID_LIMITER_MAX_TOKEN_REQUESTS`: Maximum number of token requests per ID (default: `4`)
- `ID_LIMITER_TOKEN_EXPIRATION`: Token expiration time in minutes (default: `1440`)

### Challenge configuration

- `CHALLENGE_WINDOW`: Time window in seconds for challenge validation (default: `8`)
- `CHALLENGE_DOMAIN`: Current service domain name for challenge validation (required)

### Logging

- `DEBUG`: Enable debug logging (default: `false`)

You can generate a random iDenfy signing key using:

```bash
cat /dev/urandom | tr -dc 'a-zA-Z0-9' | fold -w 32 | head -n 1
```

For implementation details, refer to `internal/configs/config.go`.

## Running the application

### Using Docker Compose

First create and set the environment variables in `.app.env` and `.db.env`. Examples can be found in `.app.env.example` and `.db.env.example`. In beta releases, the mongo-express container is included but can be disabled.

To start only the core services (API and MongoDB):

```bash
docker compose up -d
```

To include mongo-express for development, create `.express.env` as well, then run:

```bash
docker compose -f docker-compose.yml -f docker-compose.dev.yml up -d
```

### Running locally

1. Ensure MongoDB is running and accessible.
2. Export the environment variables:

    ```bash
    set -a
    source .app.env
    set +a
    ```

3. Run the application:

    ```bash
    go run cmd/api/main.go
    ```

## API endpoints

### Authentication headers

When authentication is required, include these headers:

| Header | Description | Required |
|--------|-------------|----------|
| `X-Client-ID` | The Ledger Chain SS58Address of the client | Yes |
| `X-Challenge` | Hex-encoded message `{api-domain}:{timestamp}` | Yes |
| `X-Signature` | Hex-encoded sr25519 or ed25519 signature of the challenge | Yes |

For sponsee authentication, include these additional headers:

| Header | Description | Required |
|--------|-------------|----------|
| `X-Sponsee-ID` | The Ledger Chain SS58Address of the sponsee | Yes |
| `X-Sponsee-Challenge` | Hex-encoded message `{api-domain}:{timestamp}` | Yes |
| `X-Sponsee-Signature` | Hex-encoded sr25519 or ed25519 signature of the sponsee | Yes |

### Endpoint authentication requirements

| Endpoint | Method | Authentication Required | Notes |
|----------|--------|-------------------------|-------|
| `/api/v1/token` | POST | Yes | Standard authentication |
| `/api/v1/data` | GET | Yes | Standard authentication |
| `/api/v1/sponsorships` | POST | Yes | Both sponsor and sponsee authentication |

### Sponsorships

#### Create sponsorship

- `POST /api/v1/sponsorships`
  - Creates a new sponsorship between a KYC-verified sponsor and a sponsee
  - Responses: `201` Success, `400` Bad request, `401` Unauthorized, `403` Forbidden, `404` Not found, `409` Conflict

#### List sponsorships

- `GET /api/v1/sponsorships`
  - Query parameters: `sponsor_twin_id`, `sponsee_twin_id`, `sponsor_client_id`, `sponsee_client_id`, `limit`, `offset`
  - Responses: `200` Success, `400` Bad request

### Verification

#### Get verification token

- `POST /api/v1/token`
  - Get or create a verification token
  - Responses: `200` Existing token, `201` New token, `400` Bad request, `401` Unauthorized, `402` Payment required, `403` Forbidden, `409` Conflict

#### Get verification data

- `GET /api/v1/data`
  - Get verification data for a client
  - Responses: `200` Success, `400` Bad request, `401` Unauthorized, `404` Not found

#### Get verification status

- `GET /api/v1/status`
  - Query parameters: `client_id` or `twin_id` (at least one required)
  - Responses: `200` Success, `400` Bad request, `404` Not found

### Service information

#### Health check

- `GET /api/v1/health`
  - Responses: `200` Healthy or degraded, `503` Service unavailable

#### Get service configs

- `GET /api/v1/configs`
  - Returns current service configuration (sensitive values redacted)

#### Get service version

- `GET /api/v1/version`
  - Returns application version

#### API documentation

- `GET /docs`
  - Swagger documentation interface

### Webhook endpoints

- `POST /webhooks/idenfy/verification-update`
  - Process verification update from iDenfy
  - Required header: `Idenfy-Signature`

- `POST /webhooks/idenfy/id-expiration`
  - Process document expiration notification (not implemented)
  - Response: `501` Not implemented

Refer to the Swagger documentation at `/docs` for detailed request/response formats.

## Development

### Local development with ngrok

For local development, you can use ngrok to receive iDenfy webhook callbacks:

1. Install ngrok and configure your authtoken.
2. Start ngrok:

   ```bash
   ngrok http http://localhost:8080
   ```

3. Update `.app.env`:

   ```env
   CHALLENGE_DOMAIN=your-ngrok-url.ngrok.io
   IDENFY_CALLBACK_URL=https://your-ngrok-url.ngrok.io/webhooks/idenfy/verification-update
   ```

4. Restart the application.

Alternatively, use `localtunnel`:

```bash
npx localtunnel --port 8080 --subdomain your-subdomain
```

### Project structure

```text
.
├── api/                    # API documentation (Swagger)
├── cmd/                    # Main application entry points
│   └── api/                # API server entry point
├── configs/                # Configuration files
├── deployments/            # Deployment configurations
├── docs/                   # Documentation
├── internal/               # Private application code
│   ├── clients/            # External service clients
│   ├── config/             # Configuration handling
│   ├── handlers/           # HTTP request handlers
│   ├── middleware/         # HTTP middleware
│   ├── models/             # Data models
│   ├── repository/         # Data access layer
│   ├── responses/          # Response formatters
│   └── services/           # Business logic
├── scripts/                # Utility scripts
├── .app.env.example        # Example environment variables
├── .db.env.example         # Example database environment variables
├── docker-compose.yml      # Docker Compose configuration
└── go.mod                  # Go module definition
```

## Testing

Run unit tests:

```bash
go test ./...
```

Run tests with coverage:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out -o coverage.html
```

Run a specific test:

```bash
go test -run TestFunctionName
```

## Building the Docker image

```bash
docker build -t tf_kyc_verifier .
```

## Running the Docker container

```bash
docker run -d -p 8080:8080 --env-file .app.env tf_kyc_verifier
```

## Database backup and restore

### Creating a dump

```bash
#!/bin/bash
source .db.env
docker exec tf_kyc_db mongodump --username $MONGO_INITDB_ROOT_USERNAME --password $MONGO_INITDB_ROOT_PASSWORD --authenticationDatabase admin --db tfgrid-kyc-db --archive=mongo.kyc.archive.dump
docker cp tf_kyc_db:/mongo.kyc.archive.dump mongo.kyc.archive.dump
```

### Restoring a dump

```bash
#!/bin/bash
source .db.env
docker cp mongo.kyc.archive.dump tf_kyc_db:/mongo.kyc.archive.dump
docker exec tf_kyc_db mongorestore --username $MONGO_INITDB_ROOT_USERNAME --password $MONGO_INITDB_ROOT_PASSWORD --authenticationDatabase admin --nsInclude='tfgrid-kyc-db.*' --archive=mongo.kyc.archive.dump
```

## Production

Refer to the [Production Setup](./docs/production.md) documentation for production setup details.

## Contributing

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/your-feature-name`
3. Make your changes
4. Run tests: `make test`
5. Run linter: `make lint`
6. Format your code: `make fmt`
7. Generate swagger docs when needed: `make swagger`
8. Commit your changes: `git commit -m "Add your feature"`
9. Push to the branch: `git push origin feature/your-feature-name`
10. Open a pull request

### Makefile commands

- `make test`: Run all unit tests
- `make lint`: Run the linter
- `make fmt`: Format Go source code
- `make swagger`: Generate or update Swagger API documentation
- `make help`: Display available commands

### Code style

- Follow standard Go formatting
- Write tests for new functionality
- Document public functions and types
- Keep commits focused and atomic

## License

This project is licensed under the Apache License 2.0 — see the [LICENSE](LICENSE) file for details.
Copyright (c) TFTech NV.
