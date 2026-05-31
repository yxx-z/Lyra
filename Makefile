.PHONY: build build-frontend test dev-backend dev-frontend docker-build clean

build: build-frontend
	go build -o lyra ./cmd/server

build-frontend:
	cd web && npm run build

test:
	go test ./...

dev-backend:
	go run ./cmd/server --config config.yaml

dev-frontend:
	cd web && npm run dev

docker-build:
	docker build -t lyra:latest .

clean:
	rm -f lyra
	rm -rf ui/dist/*
	touch ui/dist/.gitkeep
