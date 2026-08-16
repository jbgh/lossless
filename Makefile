.PHONY: test cover cover-html vet

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
