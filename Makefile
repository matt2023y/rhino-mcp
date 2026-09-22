.DEFAULT_GOAL := help
DOTNET ?= dotnet
YAK ?= $(CURDIR)/yak
YAK_PLATFORM ?= any
YAK_DOTNET_ROOT ?= $(if $(DOTNET_ROOT),$(DOTNET_ROOT),$(shell $(DOTNET) --list-runtimes 2>/dev/null | sed -n 's|^Microsoft.NETCore.App 8\.[^[]*\[\(.*\)/shared/Microsoft.NETCore.App\]$$|\1|p' | head -n 1))
.PHONY: help build build-windows build-macos test plugin plugin-test config server package package-windows package-macos package-plugin package-assemble-windows package-assemble-macos

help:
	@echo "build / build-windows; build-macos; package: both platforms; package-windows; package-macos; package-plugin: existing RHP to Yak; package-assemble-windows / package-assemble-macos: package existing builds; test; plugin; plugin-test; config; server"

build: build-windows build-macos plugin

build-windows: export GOOS=windows
build-windows: export GOARCH=amd64
build-windows: export CGO_ENABLED=0
build-windows:
	go build -trimpath -ldflags="-s -w" -o .build/windows/rhino-mcp-server.exe ./cmd/rhino-mcp-server
	go build -trimpath -ldflags="-s -w" -o .build/windows/rhino-tool.exe ./cmd/rhino-tool

build-macos: export GOOS=darwin
build-macos: export CGO_ENABLED=0
build-macos:
	GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o .build/macos/amd64/rhino-mcp-server ./cmd/rhino-mcp-server
	GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o .build/macos/amd64/rhino-tool ./cmd/rhino-tool
	GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o .build/macos/arm64/rhino-mcp-server ./cmd/rhino-mcp-server
	GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o .build/macos/arm64/rhino-tool ./cmd/rhino-tool
	lipo -create .build/macos/amd64/rhino-mcp-server .build/macos/arm64/rhino-mcp-server -output .build/macos/rhino-mcp-server
	lipo -create .build/macos/amd64/rhino-tool .build/macos/arm64/rhino-tool -output .build/macos/rhino-tool
	codesign --force --sign - .build/macos/rhino-mcp-server .build/macos/rhino-tool

test:
	go test ./...

config:
	go run ./cmd/rhino-tool config

server:
	go run ./cmd/rhino-mcp-server

plugin:
	$(DOTNET) build plugin/RhinoMcpPlugin.csproj -c Release

plugin-test:
	$(DOTNET) run --project plugin/tests/ConfigTests.csproj

package-plugin: plugin
	@set -eu; \
	mkdir -p .build dist/yak; \
	stage=$$(mktemp -d "$(CURDIR)/.build/yak.XXXXXX"); \
	trap 'rm -rf "$$stage"' EXIT; \
	cp plugin/manifest.yml plugin/bin/Release/net7.0/RhinoMcpPlugin.rhp plugin/bin/Release/net7.0/RhinoMcpPlugin.deps.json "$$stage/"; \
	cd "$$stage"; \
	$(if $(YAK_DOTNET_ROOT),DOTNET_ROOT="$(YAK_DOTNET_ROOT)") "$(abspath $(YAK))" build --platform "$(YAK_PLATFORM)"; \
	cp ./*.yak "$(CURDIR)/dist/yak/"; \
	rm -rf "$(CURDIR)/.build/yak-package"; \
	mkdir -p "$(CURDIR)/.build/yak-package"; \
	cp ./*.yak "$(CURDIR)/.build/yak-package/"

package:  package-plugin package-windows package-macos

package-windows: build-windows
	$(MAKE) package-assemble-windows

package-macos: build-macos
	$(MAKE) package-assemble-macos

# Explicit file list: never copy local credentials, model logs or caches.
# Finish staging and ZIP creation before replacing previously published outputs.
package-assemble-windows package-assemble-macos:
	@set -eu; \
	platform="$(@:package-assemble-%=%)"; \
	skill=rhino-mcp-modeling; \
	case "$$platform" in \
		windows) ext=.exe; archive="$$skill-windows-amd64.zip" ;; \
		macos) ext=; archive="$$skill-macos-universal.zip" ;; \
	esac; \
	set -- .build/yak-package/*.yak; \
	if [ "$$#" -ne 1 ] || [ ! -f "$$1" ]; then \
		echo 'Expected one current Yak package in .build/yak-package; run make package-plugin first' >&2; exit 1; \
	fi; \
	yak="$$1"; \
	for source in "skills/$$skill/SKILL.md" "skills/$$skill/scripts/inspect.py" \
		".build/$$platform/rhino-mcp-server$$ext" ".build/$$platform/rhino-tool$$ext" \
		.env.example "skills/$$skill/models-logs/.gitkeep"; do \
		if [ ! -f "$$source" ]; then echo "Missing package file: $$source" >&2; exit 1; fi; \
	done; \
	dist="$(CURDIR)/dist/$$platform"; \
	mkdir -p "$$dist"; \
	stage=$$(mktemp -d "$$dist/.package-XXXXXX"); \
	trap 'rm -rf "$$stage"' EXIT; \
	mkdir -p "$$stage/$$skill/bin" "$$stage/$$skill/scripts" "$$stage/$$skill/models-logs"; \
	cp -p "skills/$$skill/SKILL.md" .env.example "$$stage/$$skill/"; \
	cp -p "skills/$$skill/scripts/inspect.py" "$$stage/$$skill/scripts/"; \
	cp -p "skills/$$skill/models-logs/.gitkeep" "$$stage/$$skill/models-logs/"; \
	cp -p ".build/$$platform/rhino-mcp-server$$ext" ".build/$$platform/rhino-tool$$ext" "$$yak" "$$stage/$$skill/bin/"; \
	if [ "$$platform" = macos ]; then chmod 755 "$$stage/$$skill/bin/rhino-mcp-server" "$$stage/$$skill/bin/rhino-tool"; fi; \
	(cd "$$stage" && zip -q -X "$$archive" \
		"$$skill/SKILL.md" "$$skill/scripts/inspect.py" \
		"$$skill/bin/rhino-mcp-server$$ext" "$$skill/bin/rhino-tool$$ext" \
		"$$skill/.env.example" "$$skill/models-logs/.gitkeep" "$$skill/bin/$${yak##*/}"); \
	rm -rf "$$dist/$$skill"; \
	mv "$$stage/$$skill" "$$dist/$$skill"; \
	mv -f "$$stage/$$archive" "$$dist/$$archive"; \
	echo "Skill: dist/$$platform/$$skill"; \
	echo "ZIP:   dist/$$platform/$$archive"
