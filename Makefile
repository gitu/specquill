.PHONY: build web server dev-fixture test clean

build: web server                ## release build: SPA embedded in single binary

web:                             ## build the SPA into the server's embed dir
	cd web && npm run build

server:                          ## build the Go server (embeds web build)
	cd server && CGO_ENABLED=0 go build -o specquill ./cmd/specquill

dev-fixture:                     ## create local bare origin repos under data/origin/
	./scripts/dev-fixture.sh

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
