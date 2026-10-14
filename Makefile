.PHONY: build web processor test serve smoke
build: web
	go build -o weer ./cmd/weer
web:
	cd web && pnpm install --frozen-lockfile && pnpm build
processor:
	uv venv --python 3.12 .venv
	uv pip install --python .venv/bin/python -r processor/requirements.lock
	uv pip install --python .venv/bin/python --no-deps -e processor
test:
	go test ./...
	.venv/bin/python -m unittest discover -s processor/tests -v
	cd web && pnpm test
serve:
	./weer serve --config configs/weer.yaml
smoke:
	bash scripts/smoke.sh
