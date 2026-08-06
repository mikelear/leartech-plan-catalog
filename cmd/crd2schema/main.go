// Command crd2schema extracts the openAPIV3Schema from one or more Kubernetes
// CRD YAML files and writes a standalone JSON Schema per (kind, version), named
// "<kind-lower>_<version>.json", into an output directory. kubeconform then
// validates submitted Plans against these schemas at PR time — so a submission
// is checked against the SAME schema the apiserver enforces (types, enums,
// required, patterns, unknown fields under -strict).
//
// Generation lives here (Go, in-repo) rather than a Python side-script so the
// whole toolchain stays Go and reproducible. Run it whenever the vendored CRDs
// under schemas/crd/ are refreshed:
//
//	go run ./cmd/crd2schema -out schemas schemas/crd/*.yaml
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type crd struct {
	Spec struct {
		Names struct {
			Kind string `yaml:"kind"`
		} `yaml:"names"`
		Versions []struct {
			Name   string `yaml:"name"`
			Schema struct {
				OpenAPIV3Schema map[string]any `yaml:"openAPIV3Schema"`
			} `yaml:"schema"`
		} `yaml:"versions"`
	} `yaml:"spec"`
}

func main() {
	out := flag.String("out", "schemas", "output directory for generated JSON schemas")
	flag.Parse()
	if flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: crd2schema -out <dir> <crd.yaml>...")
		os.Exit(2)
	}
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fatal(err)
	}
	for _, path := range flag.Args() {
		data, err := os.ReadFile(path)
		if err != nil {
			fatal(err)
		}
		var c crd
		if err := yaml.Unmarshal(data, &c); err != nil {
			fatal(fmt.Errorf("%s: %w", path, err))
		}
		kind := c.Spec.Names.Kind
		if kind == "" {
			fatal(fmt.Errorf("%s: no spec.names.kind", path))
		}
		for _, v := range c.Spec.Versions {
			schema := normalize(v.Schema.OpenAPIV3Schema)
			schema["$schema"] = "http://json-schema.org/draft-07/schema#"
			b, err := json.MarshalIndent(schema, "", "  ")
			if err != nil {
				fatal(err)
			}
			name := fmt.Sprintf("%s_%s.json", strings.ToLower(kind), v.Name)
			dst := filepath.Join(*out, name)
			if err := os.WriteFile(dst, append(b, '\n'), 0o644); err != nil {
				fatal(err)
			}
			fmt.Printf("wrote %s (%s/%s)\n", dst, kind, v.Name)
		}
	}
}

// normalize walks the OpenAPI v3 schema and makes it a plain JSON-Schema a
// generic validator accepts: it drops x-kubernetes-* extension keys and turns
// int-or-string nodes into an explicit {type: [integer, string]} union.
func normalize(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	if _, intOrString := m["x-kubernetes-int-or-string"]; intOrString {
		return map[string]any{"type": []any{"integer", "string"}}
	}
	out := make(map[string]any, len(m))
	for k, val := range m {
		if strings.HasPrefix(k, "x-kubernetes-") {
			continue
		}
		switch child := val.(type) {
		case map[string]any:
			out[k] = normalize(child)
		case []any:
			arr := make([]any, 0, len(child))
			for _, e := range child {
				if em, ok := e.(map[string]any); ok {
					arr = append(arr, normalize(em))
				} else {
					arr = append(arr, e)
				}
			}
			out[k] = arr
		default:
			out[k] = val
		}
	}
	return out
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "crd2schema:", err)
	os.Exit(1)
}
