package main

import (
	"encoding/json"
	"errors"
	"fmt"

	"gopkg.in/yaml.v3"
)

// createPlanRequest mirrors plan-api's dto.CreatePlanRequest — the JSON body of
// POST /plans. It is deliberately a small projection of a Plan CRD: plan-api owns
// validation, injection, paused-defaulting and triggeredBy, so this tool only
// projects the shape and forwards. Fields/tags MUST track plan-api's DTO.
type createPlanRequest struct {
	Name   string           `json:"name"`
	Tenant string           `json:"tenant,omitempty"`
	Steps  []createPlanStep `json:"steps"`
}

// createPlanStep mirrors plan-api's dto.CreatePlanStep. With/Inputs are raw JSON
// (opaque to plan-api, stored verbatim). Hold/FanIn are always emitted (the DTO
// carries them as plain bools).
type createPlanStep struct {
	Name          string          `json:"name"`
	Kind          string          `json:"kind,omitempty"`
	Use           string          `json:"use,omitempty"`
	With          json.RawMessage `json:"with,omitempty"`
	AgentType     string          `json:"agentType,omitempty"`
	Inputs        json.RawMessage `json:"inputs,omitempty"`
	Repo          string          `json:"repo,omitempty"`
	DependsOn     []string        `json:"dependsOn,omitempty"`
	Hold          bool            `json:"hold"`
	FanIn         bool            `json:"fanIn"`
	FanInValidate []string        `json:"fanInValidate,omitempty"`
	BudgetIter    *int            `json:"budgetIter,omitempty"`
}

// planDoc is the minimal decode target for a catalog Plan CRD — only the fields
// the DTO projection reads. Unknown fields (spec.paused, apiVersion, etc.) are
// intentionally ignored: paused is plan-api's to default, and this tool never
// round-trips the full CRD.
type planDoc struct {
	Kind     string `yaml:"kind"`
	Metadata struct {
		Name string `yaml:"name"`
	} `yaml:"metadata"`
	Spec struct {
		Tenant string     `yaml:"tenant"`
		Steps  []planStep `yaml:"steps"`
	} `yaml:"spec"`
}

type planStep struct {
	Name          string         `yaml:"name"`
	Kind          string         `yaml:"kind"`
	Use           string         `yaml:"use"`
	With          map[string]any `yaml:"with"`
	AgentType     string         `yaml:"agentType"`
	Inputs        map[string]any `yaml:"inputs"`
	Repo          string         `yaml:"repo"`
	DependsOn     []string       `yaml:"dependsOn"`
	Hold          bool           `yaml:"hold"`
	FanIn         bool           `yaml:"fanIn"`
	FanInValidate []string       `yaml:"fanInValidate"`
	BudgetIter    *int           `yaml:"budgetIter"`
}

// project decodes a single Plan YAML document and projects it to the plan-api
// CreatePlanRequest. It rejects non-Plan kinds and structurally empty plans so a
// mis-filed template or blank file fails the release loudly rather than POSTing
// garbage. It does NOT enforce plan-api's business rules — that is plan-api's job.
func project(data []byte) (createPlanRequest, error) {
	var doc planDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return createPlanRequest{}, fmt.Errorf("invalid YAML: %w", err)
	}
	if doc.Kind != "" && doc.Kind != "Plan" {
		return createPlanRequest{}, fmt.Errorf("kind %q is not a Plan (submit only handles plans/; templates sync separately)", doc.Kind)
	}
	if doc.Metadata.Name == "" {
		return createPlanRequest{}, errors.New("metadata.name is required")
	}
	if len(doc.Spec.Steps) == 0 {
		return createPlanRequest{}, errors.New("spec.steps must declare at least one step")
	}

	req := createPlanRequest{Name: doc.Metadata.Name, Tenant: doc.Spec.Tenant}
	for i, s := range doc.Spec.Steps {
		with, err := toRawJSON(s.With)
		if err != nil {
			return createPlanRequest{}, fmt.Errorf("step %d (%s) with: %w", i, s.Name, err)
		}
		inputs, err := toRawJSON(s.Inputs)
		if err != nil {
			return createPlanRequest{}, fmt.Errorf("step %d (%s) inputs: %w", i, s.Name, err)
		}
		req.Steps = append(req.Steps, createPlanStep{
			Name:          s.Name,
			Kind:          s.Kind,
			Use:           s.Use,
			With:          with,
			AgentType:     s.AgentType,
			Inputs:        inputs,
			Repo:          s.Repo,
			DependsOn:     s.DependsOn,
			Hold:          s.Hold,
			FanIn:         s.FanIn,
			FanInValidate: s.FanInValidate,
			BudgetIter:    s.BudgetIter,
		})
	}
	return req, nil
}

// toRawJSON marshals a decoded YAML map to json.RawMessage, or nil for an empty
// map (so an absent with/inputs is omitted rather than sent as `{}`).
func toRawJSON(m map[string]any) (json.RawMessage, error) {
	if len(m) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	return b, nil
}
