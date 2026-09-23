module github.com/nikivavlt/url-shortener/user

go 1.26.0

// Контракты берём из общего модуля shared (локально резолвится через go.work):
//   github.com/yourname/url-shortener/shared/pkg/proto/user/v1
//
// Остальные зависимости подтянутся через `go mod tidy` после первых импортов:
//   google.golang.org/grpc, google.golang.org/protobuf
//   github.com/jackc/pgx/v5 (+ pgxpool)
//   github.com/redis/go-redis/v9
//   github.com/golang-jwt/jwt/v5
//   golang.org/x/crypto/bcrypt
//   github.com/joho/godotenv      (загрузка user/.env)
//   github.com/stretchr/testify

require (
	github.com/golang-jwt/jwt/v5 v5.3.1
	github.com/golang-migrate/migrate/v4 v4.20.1
	github.com/jackc/pgx/v5 v5.11.0
	github.com/joho/godotenv v1.5.1
	github.com/nikivavlt/url-shortener/shared v0.0.0-20260920075859-1a0ec1a60887
	github.com/redis/go-redis/v9 v9.22.0
	github.com/stretchr/testify v1.12.1
	golang.org/x/crypto v0.57.0
	google.golang.org/grpc v1.84.0
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	go.uber.org/atomic v1.11.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/sync v0.23.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.42.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260706201446-f0a921348800 // indirect
	google.golang.org/protobuf v1.36.11 // indirect
)
