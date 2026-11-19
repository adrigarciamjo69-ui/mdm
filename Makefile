VERSION ?= 1.0.0
LDFLAGS = -s -w -X main.version=$(VERSION)
AGENT_TARGETS = linux/amd64 linux/arm64 windows/amd64 windows/arm64

.PHONY: test test-short build web

test:
	go test ./...

test-short:
	go test -short ./...

web:
	cd web && npm ci && npm run build

build:
	mkdir -p dist
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o dist/mdm-server ./cmd/mdm-server
	@for t in $(AGENT_TARGETS); do \
		os=$${t%/*}; arch=$${t##*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "agente $$os/$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags "$(LDFLAGS)" -o dist/mdm-agent-$$os-$$arch$$ext ./cmd/mdm-agent || exit 1; \
	done
