APP := ttysvg
CMD := ./cmd/ttysvg
DIST := dist
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf dev)
GOARM ?= 7
LDFLAGS := -s -w -X main.version=$(VERSION)

MANPAGE := docs/ttysvg.1
COMPLETIONS := completions/ttysvg.bash completions/ttysvg.fish completions/_ttysvg

RELEASE_TARGETS := \
	linux_x86 \
	linux_x86_64 \
	linux_arm \
	linux_arm64 \
	macos_x86_64 \
	macos_arm64

.PHONY: test tests bench bench-report mandoc release build-release checksums size-report clean $(RELEASE_TARGETS)

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
	@printf 'building %s\n' "$(APP)_$(VERSION)_$(2)"
	rm -rf "$(DIST)/$(APP)_$(VERSION)_$(2)"
	mkdir -p "$(DIST)/$(APP)_$(VERSION)_$(2)/man/man1" "$(DIST)/$(APP)_$(VERSION)_$(2)/completions"
	CGO_ENABLED=0 GOOS=$(3) GOARCH=$(4) $(if $(5),GOARM=$(5),) go build -trimpath -ldflags="$(LDFLAGS)" -o "$(DIST)/$(APP)_$(VERSION)_$(2)/$(APP)" $(CMD)
	cp "$(MANPAGE)" "$(DIST)/$(APP)_$(VERSION)_$(2)/man/man1/$(APP).1"
	cp completions/ttysvg.bash "$(DIST)/$(APP)_$(VERSION)_$(2)/completions/$(APP).bash"
	cp completions/ttysvg.fish "$(DIST)/$(APP)_$(VERSION)_$(2)/completions/$(APP).fish"
	cp completions/_ttysvg "$(DIST)/$(APP)_$(VERSION)_$(2)/completions/_$(APP)"
	tar -C "$(DIST)" -czf "$(DIST)/$(APP)_$(VERSION)_$(2).tar.gz" "$(APP)_$(VERSION)_$(2)"
endef

$(eval $(call release_target,linux_x86,linux_x86,linux,386,))
$(eval $(call release_target,linux_x86_64,linux_x86_64,linux,amd64,))
$(eval $(call release_target,linux_arm,linux_arm,linux,arm,$(GOARM)))
$(eval $(call release_target,linux_arm64,linux_arm64,linux,arm64,))
$(eval $(call release_target,macos_x86_64,macos_x86_64,darwin,amd64,))
$(eval $(call release_target,macos_arm64,macos_arm64,darwin,arm64,))

checksums: build-release
	@if command -v shasum >/dev/null 2>&1; then \
		cd "$(DIST)" && shasum -a 256 *.tar.gz > checksums.txt; \
	else \
		cd "$(DIST)" && sha256sum *.tar.gz > checksums.txt; \
	fi

size-report: build-release
	@mkdir -p "$(DIST)"
	@printf '%-36s %12s\n' 'binary' 'MB' | tee "$(DIST)/SIZES.txt"
	@for f in "$(DIST)"/$(APP)_$(VERSION)_*/$(APP); do \
		[ -f "$${f}" ] || continue; \
		bytes=$$(wc -c < "$${f}" | tr -d ' '); \
		name=$$(basename "$$(dirname "$${f}")"); \
		mb=$$(awk "BEGIN { printf \"%.2f\", $${bytes} / 1000000 }"); \
		printf '%-36s %12s\n' "$${name}" "$${mb}"; \
	done | tee -a "$(DIST)/SIZES.txt"

clean:
	rm -rf "$(DIST)"
