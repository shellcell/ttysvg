APP := ttysvg
CMD := ./cmd/ttysvg
DIST := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf dev)
GOARM ?= 7
LDFLAGS := -s -w -X main.version=$(VERSION)

MANPAGE := docs/ttysvg.1
COMPLETIONS := completions/ttysvg.bash completions/ttysvg.fish completions/_ttysvg

RELEASE_TARGETS := \
	linux-386 \
	linux-amd64 \
	linux-arm \
	linux-arm64 \
	darwin-amd64 \
	darwin-arm64

.PHONY: test tests bench bench-report mandoc release build-release checksums size-report packages clean $(RELEASE_TARGETS)

.NOTPARALLEL: release

test tests:
	go test ./...

# Compile and run every benchmark once (-benchtime=1x) so CI exercises the
# performance/RAM hot paths and fails on a broken benchmark, without gating on
# wall-clock numbers that vary by runner. Use `make bench BENCHTIME=2s` locally
# for real measurements.
BENCHTIME ?= 1x
bench:
	go test -run='^$$' -bench=. -benchmem -benchtime=$(BENCHTIME) ./...

# Render benchmark results as a Markdown table (as published in releases).
bench-report:
	@go test -run='^$$' -bench=. -benchmem -benchtime=$(BENCHTIME) ./... | awk -f scripts/benchtable.awk

mandoc:
	@command -v mandoc >/dev/null 2>&1 || { printf 'mandoc is required to validate %s\n' "$(MANPAGE)"; exit 1; }
	mandoc -Tlint "$(MANPAGE)"

release: clean mandoc build-release checksums size-report

build-release: $(RELEASE_TARGETS)

$(DIST):
	mkdir -p "$(DIST)"

define release_target
$(1): | $(DIST)
	@printf 'building %s\n' "$(APP)-$(VERSION)-$(1)"
	rm -rf "$(DIST)/$(APP)-$(VERSION)-$(1)"
	mkdir -p "$(DIST)/$(APP)-$(VERSION)-$(1)/man/man1" "$(DIST)/$(APP)-$(VERSION)-$(1)/completions"
	CGO_ENABLED=0 GOOS=$(2) GOARCH=$(3) $(if $(4),GOARM=$(4),) go build -trimpath -ldflags="$(LDFLAGS)" -o "$(DIST)/$(APP)-$(VERSION)-$(1)/$(APP)" $(CMD)
	cp "$(MANPAGE)" "$(DIST)/$(APP)-$(VERSION)-$(1)/man/man1/$(APP).1"
	cp LICENSE README.md "$(DIST)/$(APP)-$(VERSION)-$(1)/"
	cp completions/ttysvg.bash "$(DIST)/$(APP)-$(VERSION)-$(1)/completions/$(APP).bash"
	cp completions/ttysvg.fish "$(DIST)/$(APP)-$(VERSION)-$(1)/completions/$(APP).fish"
	cp completions/_ttysvg "$(DIST)/$(APP)-$(VERSION)-$(1)/completions/_$(APP)"
	tar -C "$(DIST)" -czf "$(DIST)/$(APP)-$(VERSION)-$(1).tar.gz" "$(APP)-$(VERSION)-$(1)"
endef

$(eval $(call release_target,linux-386,linux,386,))
$(eval $(call release_target,linux-amd64,linux,amd64,))
$(eval $(call release_target,linux-arm,linux,arm,$(GOARM)))
$(eval $(call release_target,linux-arm64,linux,arm64,))
$(eval $(call release_target,darwin-amd64,darwin,amd64,))
$(eval $(call release_target,darwin-arm64,darwin,arm64,))

checksums: build-release
	@if command -v shasum >/dev/null 2>&1; then \
		cd "$(DIST)" && shasum -a 256 *.tar.gz > checksums.txt; \
	else \
		cd "$(DIST)" && sha256sum *.tar.gz > checksums.txt; \
	fi

size-report: build-release
	@mkdir -p "$(DIST)"
	@printf '%-36s %12s\n' 'binary' 'MB' | tee "$(DIST)/SIZES.txt"
	@for f in "$(DIST)"/$(APP)-$(VERSION)-*/$(APP); do \
		[ -f "$${f}" ] || continue; \
		bytes=$$(wc -c < "$${f}" | tr -d ' '); \
		name=$$(basename "$$(dirname "$${f}")"); \
		mb=$$(awk "BEGIN { printf \"%.2f\", $${bytes} / 1000000 }"); \
		printf '%-36s %12s\n' "$${name}" "$${mb}"; \
	done | tee -a "$(DIST)/SIZES.txt"

# Build .deb/.rpm/.apk packages from the prebuilt linux binaries using nfpm.
# Covers amd64 and arm64, which is what apt/dnf users overwhelmingly need.
# Override with `make packages NFPM_ARCHES="amd64"` to limit the set.
NFPM_ARCHES ?= amd64 arm64
PKG_VERSION ?= $(patsubst v%,%,$(VERSION))

packages: | $(DIST)
	@command -v nfpm >/dev/null 2>&1 || { printf 'nfpm is required to build packages: https://nfpm.goreleaser.com\n'; exit 1; }
	@for arch in $(NFPM_ARCHES); do \
		bin="$(DIST)/$(APP)-$(VERSION)-linux-$$arch/$(APP)"; \
		[ -f "$$bin" ] || { printf 'missing %s; run make build-release first\n' "$$bin"; exit 1; }; \
		for fmt in deb rpm apk; do \
			printf 'packaging %s (%s)\n' "$$arch" "$$fmt"; \
			PKG_VERSION="$(PKG_VERSION)" PKG_ARCH="$$arch" PKG_BIN="$$bin" \
				envsubst < nfpm.yaml > "$(DIST)/nfpm.$$arch.$$fmt.yaml"; \
			nfpm package -f "$(DIST)/nfpm.$$arch.$$fmt.yaml" -p "$$fmt" -t "$(DIST)"; \
		done; \
	done

clean:
	rm -rf "$(DIST)"
