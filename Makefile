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

# Fail if the committed schemas are stale relative to the vendored CRDs.
verify:
	go run ./cmd/crd2schema -out /tmp/schemas-gen schemas/crd/*.yaml
	@for f in schemas/*.json; do \
		diff -u "$$f" "/tmp/schemas-gen/$$(basename $$f)" || \
		{ echo "schemas/ is stale — run: make schemas"; exit 1; }; \
	done
	@echo "schemas up to date"

all: test verify lint
