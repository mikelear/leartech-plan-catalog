# ShipProven Plan Catalog — dev tasks. Everything the CI hard gate runs is
# reproducible here locally (no drift).

KUBECONFORM_VERSION := v0.8.0
SCHEMA_LOCATION := ./schemas/{{ .ResourceKind }}_{{ .ResourceAPIVersion }}.json

.PHONY: test lint schemas verify all

# Unit tests for the rules engine.
test:
	go test ./...

# The deterministic hard gate: rule engine + CRD-schema conformance.
lint: schemas
	go run ./cmd/plan-lint
	go run github.com/yannh/kubeconform/cmd/kubeconform@$(KUBECONFORM_VERSION) \
		-strict -summary -schema-location '$(SCHEMA_LOCATION)' plans/ templates/

# Regenerate the JSON schemas from the vendored CRDs under schemas/crd/.
# Run this whenever the CRDs are refreshed.
schemas:
	go run ./cmd/crd2schema -out schemas schemas/crd/*.yaml

# Regenerate the rule catalog (docs/rules.json + rules.md) from pkg/planlint.
# Run this whenever the rules change.
rules:
	go run ./cmd/rulesdoc -out docs

# Fail if committed schemas or rule docs are stale relative to their sources.
verify:
	go run ./cmd/crd2schema -out /tmp/schemas-gen schemas/crd/*.yaml
	@for f in schemas/*.json; do \
		diff -u "$$f" "/tmp/schemas-gen/$$(basename $$f)" || \
		{ echo "schemas/ is stale — run: make schemas"; exit 1; }; \
	done
	go run ./cmd/rulesdoc -out /tmp/docs-gen
	@for f in rules.json rules.md; do \
		diff -u "docs/$$f" "/tmp/docs-gen/$$f" || \
		{ echo "docs/$$f is stale — run: make rules"; exit 1; }; \
	done
	@echo "schemas + rule docs up to date"

all: test verify lint
