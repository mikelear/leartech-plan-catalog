package planlint

// Public, I/O-free entry points for consumers that lint in-memory documents
// rather than a filesystem tree — notably leartech-plan-api, which validates a
// single submitted Plan on its write path and must apply the SAME rules as the
// catalog's CLI gate (one source of truth, no divergent validators).
//
// The CLI's Run() (filesystem walker) stays the catalog-side adapter; these
// wrappers promote the index-then-lint core the test harness already open-coded
// so a service never has to reimplement it.

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// BuildIndex indexes the PlanTemplate documents in docs into a TemplateIndex so
// Plans that `use:` them can be checked (required params, step names). Non-template
// docs are ignored. Use this when the templates come from somewhere other than the
// same submission (e.g. plan-api resolves them from the cluster/catalog).
func BuildIndex(docs ...map[string]any) TemplateIndex {
	idx := TemplateIndex{}
	for _, doc := range docs {
		if asStr(doc["kind"]) != "PlanTemplate" {
			continue
		}
		if name := asStr(mapOf(doc["metadata"])["name"]); name != "" {
			idx[name] = templateMeta(mapOf(doc["spec"]))
		}
	}
	return idx
}

// LintOne lints a single decoded document against a caller-supplied template
// index, returning fresh Findings. This is the service seam: plan-api builds the
// index from templates it knows (BuildIndex) and lints the submitted Plan. A nil
// index is fine (use:-checks against unknown templates simply can't cross-verify).
func LintOne(doc map[string]any, where string, idx TemplateIndex) *Findings {
	if idx == nil {
		idx = TemplateIndex{}
	}
	f := &Findings{}
	LintDoc(doc, where, f, idx)
	return f
}

// LintDocs lints a set of already-decoded documents, self-indexing any
// co-submitted PlanTemplates (the batch case: a submission carrying both a Plan
// and the templates it composes). Mirrors what Run() does per file, minus the I/O.
func LintDocs(docs ...map[string]any) *Findings {
	idx := BuildIndex(docs...)
	f := &Findings{}
	for _, doc := range docs {
		LintDoc(doc, "", f, idx)
	}
	return f
}

// LintBytes decodes a (possibly multi-document) YAML body and lints it,
// self-indexing co-submitted templates. `where` labels findings (e.g. the plan
// name or request id). A decode error is returned as an error AND recorded as an
// R1 finding, so callers can treat "unparseable submission" uniformly.
func LintBytes(where string, data []byte) (*Findings, error) {
	f := &Findings{}
	var docs []map[string]any
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc map[string]any
		if err := dec.Decode(&doc); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			f.err("R1", where, fmt.Sprintf("invalid YAML: %v", err))
			return f, fmt.Errorf("planlint: decode %s: %w", where, err)
		}
		if len(doc) > 0 {
			docs = append(docs, doc)
		}
	}
	idx := BuildIndex(docs...)
	for _, doc := range docs {
		LintDoc(doc, where, f, idx)
	}
	return f, nil
}

// HasErrors reports whether the findings contain any hard-gate error (as opposed
// to advisory warnings) — the boolean a write path gates on.
func (f *Findings) HasErrors() bool { return len(f.Errors) > 0 }
