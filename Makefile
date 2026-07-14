.PHONY: build web server dev dev-fixture test e2e dev-server clean

build: web server                ## release build: SPA embedded in single binary

web:                             ## build the SPA into the server's embed dir
	cd web && npm run build

server:                          ## build the Go server (embeds web build)
	cd server && CGO_ENABLED=0 go build -o specquill ./cmd/specquill

dev:                             ## hot-reload dev loop: postgres + air (Go rebuild) + vite (web HMR on :5173)
	./scripts/dev.sh

dev-fixture:                     ## create local bare origin repos under data/origin/
	./scripts/dev-fixture.sh

dev-samples:                     ## two extra sample spec repos with multi-commit history (auto-registers when the dev server runs)
	./scripts/dev-samples.sh

test:
	cd server && go test ./...
	cd web && npm test --silent

e2e:                             ## needs a running dev server (make dev-server)
	cd web && npx playwright test

dev-server: server dev-fixture   ## build + start the dev server with auto-auth
	./server/specquill -config specquill.dev.yml -dev

clean:
	rm -rf server/specquill server/internal/webui/dist/* web/dist
	touch server/internal/webui/dist/.gitkeep
