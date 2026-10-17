# go-grpc-inventory-service

A small Go 1.22 inventory service. It stores products, adjusts on-hand stock, and holds units with idempotent reservations. The same process serves gRPC and a gRPC-Gateway REST API.

This is a portfolio demo by Arpan Bom. It is not a production system. There is no TLS, no external identity provider, and no measured performance numbers.

## Architecture

```mermaid
flowchart LR
  grpcClient[gRPC client]
  httpClient[HTTP client]

  subgraph process [inventoryd]
    auth[Auth, logging, recovery]
    grpcAPI[gRPC server]
    gateway[gRPC-Gateway]
    svc[Inventory service]
    grpcClient --> auth
    httpClient --> gateway
    gateway --> auth
    auth --> grpcAPI
    grpcAPI --> svc
  end

  svc --> store{Store}
  store --> memory[In-memory store]
  store --> pg[(Postgres)]
```

`on_hand` is the physical quantity. `reserved` is the subset held by reservations that are still held. `available` is `on_hand - reserved`.

- Reserve decreases availability and increases `reserved`. The idempotency key makes a retry return the original reservation instead of holding the units twice. The same key with a different product or quantity is rejected.
- Commit removes the units from the warehouse: both `on_hand` and `reserved` decrease. Repeating commit returns the committed reservation.
- Release cancels a hold: `reserved` decreases and the units become available again. A committed reservation cannot be released, and a released reservation cannot be committed.

Postgres is used when `INVENTORY_DATABASE_URL` is set. Otherwise the process uses the in-memory store, which is also the fake behind the unit tests.

## Run

Requires Go 1.22.

```bash
go run ./cmd/inventoryd
```

That starts an in-memory server:

| Port | API |
| --- | --- |
| 19090 | gRPC |
| 18080 | HTTP, including `/healthz`, `/readyz`, and `/v1/...` |

The local auth token is `dev-token`. Send it as `Authorization: Bearer dev-token`. Health probes do not require it. gRPC health and server reflection do not require it either. This token is a demo default, not a secret.

Create a product and hold stock:

```bash
curl -s -H "Authorization: Bearer dev-token" -H "Content-Type: application/json" \
  -d '{"sku":"MUG-001","name":"Stoneware mug","priceCents":1800,"onHand":25}' \
  http://127.0.0.1:18080/v1/products

curl -s -H "Authorization: Bearer dev-token" -H "Content-Type: application/json" \
  -d '{"idempotencyKey":"order-1001","productId":"PRODUCT_ID","quantity":2}' \
  http://127.0.0.1:18080/v1/reservations
```

`priceCents` is an integer minor-unit amount. The service does not attach or convert a currency. JSON encodes `int64` fields such as `priceCents` as strings; a request may send either a number or a string.

With [`grpcurl`](https://github.com/fullstorydev/grpcurl):

```bash
grpcurl -plaintext -H "authorization: Bearer dev-token" \
  -d '{"sku":"MUG-001","name":"Stoneware mug","price_cents":1800,"on_hand":25}' \
  127.0.0.1:19090 inventory.v1.InventoryService/CreateProduct
```

### Postgres via Docker Compose

```bash
docker compose up --build
```

Compose starts Postgres 16 and the service. The database user, password, and database name are all `inventory`. That password exists only for this local demo. Migrations run when the process opens the pool. SQL files live in `internal/repository/postgres/migrations`.

The process reads environment variables directly and does not load a dotenv file. `.env.example` lists the names. Export `INVENTORY_DATABASE_URL` before `go run` if you want the host-built binary to use Compose's Postgres:

```bash
export INVENTORY_DATABASE_URL=postgres://inventory:inventory@localhost:5432/inventory?sslmode=disable
go run ./cmd/inventoryd
```

Shutdown on `SIGINT` or `SIGTERM` marks gRPC health as not serving, drains HTTP, then stops gRPC.

## API

| RPC | HTTP |
| --- | --- |
| `CreateProduct` | `POST /v1/products` |
| `GetProduct` | `GET /v1/products/{id}` |
| `ListProducts` | `GET /v1/products?pageSize=50&pageToken=` |
| `AdjustStock` | `POST /v1/products/{product_id}:adjustStock` |
| `ReserveStock` | `POST /v1/reservations` |
| `GetReservation` | `GET /v1/reservations/{id}` |
| `CommitReservation` | `POST /v1/reservations/{id}:commit` |
| `ReleaseReservation` | `POST /v1/reservations/{id}:release` |

`pageSize` defaults to 50 and cannot be greater than 100. `pageToken` is an offset returned by the previous page. List order is creation order.

SKU values are case-sensitive and limited to letters, digits, `.`, `_`, and `-`.

## Test

Unit tests use the in-memory repository and bufconn. They do not need Postgres:

```bash
go test ./...
```

Postgres integration tests are behind the `integration` build tag. They need a database and `INVENTORY_TEST_DATABASE_URL`:

```bash
export INVENTORY_TEST_DATABASE_URL=postgres://inventory:inventory@localhost:5432/inventory?sslmode=disable
go test -tags=integration ./...
```

GitHub Actions runs `go vet`, golangci-lint, `go test ./...`, and the tagged integration tests against a Postgres 16 service container. See `.github/workflows/ci.yml`.

Regenerate protobuf code with buf 1.47, `protoc-gen-go` v1.34.2, `protoc-gen-go-grpc` v1.5.1, and `protoc-gen-grpc-gateway` v2.22.0:

```bash
make generate
```

Generated Go is committed under `gen/`, so a normal build does not need buf.

## Performance

Not measured. Placeholder: this demo does not claim latency, throughput, or capacity numbers.

## Layout

- `proto/inventory/v1` — buf module and service contract
- `cmd/inventoryd` — process entrypoint
- `internal/service` — validation and reservation rules
- `internal/repository/memory` — in-memory store used as the test fake and the no-database runtime
- `internal/repository/postgres` — pgx store and embedded migrations
- `internal/grpcserver` — gRPC handlers, health, reflection
- `internal/httpserver` — REST gateway and health probes
- `internal/interceptor` — recovery, logging, static bearer auth

## License

MIT. See [LICENSE](LICENSE).
