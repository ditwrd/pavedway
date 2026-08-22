// Package catalog parses and serializes Backstage-compatible
// catalog-info.yaml entity manifests (ADR 0001: strict Backstage
// compatibility).
package catalog

import (
	"errors"
	"slices"

	"go.yaml.in/yaml/v3"
)

var supportedAPIVersion = []string{"backstage.io/v1alpha1", "pavedway.io/v1alpha1"}

type rawEntity struct {
	APIVersion string         `yaml:"apiVersion"`
	Kind       string         `yaml:"kind"`
	Metadata   map[string]any `yaml:"metadata"`
	Spec       map[string]any `yaml:"spec"`
}

type Entity struct {
	Kind       string
	Name       string
	Namespace  string
	APIVersion string
	Metadata   map[string]any
	Spec       map[string]any
}

// ToYAML serializes the entity back to catalog-info.yaml bytes.
func (e *Entity) ToYAML() ([]byte, error) {
	raw := rawEntity{
		APIVersion: e.APIVersion,
		Kind:       e.Kind,
		Metadata:   e.Metadata,
		Spec:       e.Spec,
	}

	rawByte, err := yaml.Marshal(raw)
	if err != nil {
		return nil, err
	}

	return rawByte, nil
}

func Load(data []byte) (*Entity, error) {
	var raw rawEntity

	err := yaml.Unmarshal(data, &raw)
	if err != nil {
		return nil, err
	}
	apiVersion := raw.APIVersion
	if apiVersion == "" {
		return nil, errors.New("fail to parse apiVersion")
	}

	if !slices.Contains(supportedAPIVersion, apiVersion) {
		return nil, errors.New("apiVersion not supported")
	}

	name, ok := raw.Metadata["name"].(string)
	if !ok {
		return nil, errors.New("fail to parse name")
	}
	if name == "" {
		return nil, errors.New("name is empty")
	}

	namespace, ok := raw.Metadata["namespace"].(string)
	if !ok {
		namespace = "default"
	}

	return &Entity{
		Kind:       raw.Kind,
		Name:       name,
		Namespace:  namespace,
		APIVersion: raw.APIVersion,
		Metadata:   raw.Metadata,
		Spec:       raw.Spec,
	}, nil
}
