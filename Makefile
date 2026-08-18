VERSION ?= 0.1.1
LDFLAGS := -s -w -X lossless/internal/version.Version=$(VERSION)

.PHONY: test cover cover-html vet bench lossless dist

lossless:
	go build -trimpath -ldflags "$(LDFLAGS)" -o lossless ./cmd/lossless

dist:
	VERSION=$(VERSION) scripts/dist.sh

test:
	go test ./...

vet:
	go vet ./...

cover:
	go test ./... -covermode=atomic -coverprofile=coverage.out
	@go tool cover -func=coverage.out | awk '/^total:/{print; next} {print}' | sort -k3 -n
	@echo
	@go tool cover -func=coverage.out | tail -1

cover-html: cover
	go tool cover -html=coverage.out -o coverage.html
	@echo "wrote coverage.html"

bench:
	go test ./eval/ -run 'TestSimBenchmarkSuite|TestStress|TestYearCorpus' -count=1 -timeout 180s -v
	go run ./cmd/lossless bench --root testdata/bench
