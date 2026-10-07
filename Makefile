CONTRACTS_JSON := \
	  Fixture.sol/Fixture.json
COMBINED_ABI   := $(abspath combined-abi.json)
FIXTURE_GO     := fixture/fixture.go

CONTRACTS_ROOT := $(abspath fixture/contracts)
CONTRACTS_OUT  := $(abspath fixture/contracts/out)
CONTRACTS_ARTIFACT := $(CONTRACTS_OUT)/$(CONTRACTS_JSON)

DATABASE_STAMP := db/.stamp

GOBIN_STAMP    := tool/bin/.stamp
GOBIN          := $(abspath $(dir $(GOBIN_STAMP)))

all: $(COMBINED_ABI) $(FIXTURE_GO) $(DATABASE_STAMP)

$(COMBINED_ABI): $(CONTRACTS_ARTIFACT)
	jq -s 'map({key: (input_filename | split("/") | last | sub("\\.json$$"; "")), value: .abi}) | from_entries' $(patsubst %,$(CONTRACTS_OUT)/%,$(CONTRACTS_JSON)) > $@

$(CONTRACTS_ARTIFACT): $(GOBIN_STAMP)
	forge build --root $(CONTRACTS_ROOT) --out $(CONTRACTS_OUT)

$(FIXTURE_GO): $(CONTRACTS_ARTIFACT) $(GOBIN_STAMP) tool/abigen.sh
	mkdir -p $(dir $@)
	sh tool/abigen.sh $(GOBIN)/abigen $< fixture $@

$(DATABASE_STAMP): sqlc.yml $(wildcard db/migration/*.sql) $(wildcard db/query/*.sql) $(GOBIN_STAMP)
	$(GOBIN)/sqlc generate
	git apply tool/0001-simplify-apply-log-edit-argument.patch
	git apply tool/0002-select-logs-window-hash-type.patch
	@touch $@

$(GOBIN_STAMP):
	GOBIN=$(GOBIN) go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
	GOBIN=$(GOBIN) go install github.com/pressly/goose/v3/cmd/goose@latest
	GOBIN=$(GOBIN) go install -tags untested_go_version github.com/ethereum/go-ethereum/cmd/abigen@v1.17.5
	@touch $@

test: e2e-test ## run all tests
unit-test: ## run unit tests
	go test ./... -cover
e2e-test: ## run end to end tests (and unit tests)
	go test -tags e2e ./... -cover
help: ## print this help
	@sed \
		-e '/^[a-zA-Z0-9_\-]*:.*##/!d' \
		-e 's/:.*##\s*/:/' \
		-e 's/^\(.\+\):\(.*\)/$(shell tput setaf 4)\1$(shell tput sgr0):\2/' \
		$(MAKEFILE_LIST) | column -c2 -t -s :

.PHONY: unit-test e2e-test
