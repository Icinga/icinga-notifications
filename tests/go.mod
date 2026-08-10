module github.com/icinga/icinga-notifications/tests

go 1.21

replace (
	github.com/icinga/icinga-notifications => ../
	github.com/icinga/icinga-testing => github.com/icinga/icinga-testing v0.0.0-20240118133544-4162f5a0a1f1
)

require (
	github.com/icinga/icinga-notifications v0.0.0-20240102102116-0d6f7271c116
	github.com/icinga/icinga-testing v0.0.0-20240112095229-18da8922599a
	github.com/jmoiron/sqlx v1.3.5
	github.com/stretchr/testify v1.8.4
)

require (
	github.com/Icinga/go-libs v0.0.0-20220420130327-ef58ad52edd8 // indirect
	github.com/Microsoft/go-winio v0.6.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/distribution/reference v0.5.0 // indirect
	github.com/docker/distribution v2.8.3+incompatible // indirect
	github.com/docker/docker v24.0.7+incompatible // indirect
	github.com/docker/go-connections v0.5.0 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/go-redis/redis/v8 v8.11.5 // indirect
	github.com/go-sql-driver/mysql v1.7.1 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/google/uuid v1.3.0 // indirect
	github.com/icinga/icingadb v1.1.1-0.20230418113126-7c4b947aad3a // indirect
	github.com/lib/pq v1.10.9 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.0-rc5 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.26.0 // indirect
	golang.org/x/exp v0.0.0-20220613132600-b0d781184e0d // indirect
	golang.org/x/mod v0.14.0 // indirect
	golang.org/x/sync v0.6.0 // indirect
	golang.org/x/sys v0.16.0 // indirect
	golang.org/x/tools v0.17.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)
