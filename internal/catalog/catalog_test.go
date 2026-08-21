package catalog_test

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ditwrd/pavedway/internal/catalog"
)

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/catalog-info.yaml")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

// Ticket #22 AC3: a real, unmodified Backstage catalog-info.yaml loads into
// pavedway without errors, with its fields intact.
func TestLoad_BackstageCatalogInfo(t *testing.T) {
	e, err := catalog.Load(readFixture(t))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	if e.Kind != "Component" {
		t.Fatalf("Kind = %q, want %q", e.Kind, "Component")
	}
	if e.Name != "artist-web" {
		t.Fatalf("Name = %q, want %q", e.Name, "artist-web")
	}
	if e.Namespace != "default" {
		t.Fatalf("Namespace = %q, want %q (default when metadata.namespace is absent)", e.Namespace, "default")
	}
	if got, _ := e.Metadata["description"].(string); got != "The artist web" {
		t.Fatalf("Metadata.description = %q, want %q", got, "The artist web")
	}

	spec := e.Spec
	for key, want := range map[string]any{
		"type":      "website",
		"lifecycle": "production",
		"owner":     "team-a",
	} {
		if spec[key] != want {
			t.Fatalf("Spec[%q] = %v, want %v", key, spec[key], want)
		}
	}
}

// Ticket #22 AC4: an annotation not recognized by pavedway is preserved on
// the entity after load, not dropped.
func TestLoad_UnknownAnnotationsPreserved(t *testing.T) {
	e, err := catalog.Load(readFixture(t))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	// Normalize through JSON so the assertion is independent of whether the
	// loader's yaml library yields map[string]any or map[string]string.
	raw, err := json.Marshal(e.Metadata["annotations"])
	if err != nil {
		t.Fatalf("marshal annotations: %v", err)
	}
	var annotations map[string]string
	if err := json.Unmarshal(raw, &annotations); err != nil {
		t.Fatalf("unmarshal annotations: %v", err)
	}

	for k, want := range map[string]string{
		"backstage.io/techdocs-ref": "dir:.", // TechDocs-specific: unknown to pavedway
		"github.com/project-slug":   "example/artist-web",
		"example.com/custom-tag":    "keep-me",
	} {
		if got := annotations[k]; got != want {
			t.Fatalf("annotation %q = %q, want %q (must survive byte-for-byte)", k, got, want)
		}
	}
}

// Ticket #22 AC3: fields and annotations survive a YAML round-trip within
// the supported kind — serialize the loaded entity and re-load the output.
func TestRoundTrip_FieldsAndAnnotationsSurvive(t *testing.T) {
	e1, err := catalog.Load(readFixture(t))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}

	out, err := e1.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML() error = %v, want nil", err)
	}
	if !strings.Contains(string(out), "example.com/custom-tag") {
		t.Fatalf("ToYAML() output dropped the unknown annotation:\n%s", out)
	}

	e2, err := catalog.Load(out)
	if err != nil {
		t.Fatalf("Load(ToYAML output) error = %v, want nil", err)
	}

	if e2.Kind != e1.Kind || e2.Name != e1.Name || e2.Namespace != e1.Namespace {
		t.Fatalf("identity after round-trip = %s:%s/%s, want %s:%s/%s",
			e2.Kind, e2.Namespace, e2.Name, e1.Kind, e1.Namespace, e1.Name)
	}
	if e1.APIVersion != "backstage.io/v1alpha1" {
		t.Fatalf("APIVersion = %q, want %q", e1.APIVersion, "backstage.io/v1alpha1")
	}
	if e2.APIVersion != e1.APIVersion {
		t.Fatalf("APIVersion after round-trip = %q, want %q", e2.APIVersion, e1.APIVersion)
	}
	if !reflect.DeepEqual(e1.Metadata, e2.Metadata) {
		t.Fatalf("Metadata after round-trip:\n got %#v\nwant %#v", e2.Metadata, e1.Metadata)
	}
	if !reflect.DeepEqual(e1.Spec, e2.Spec) {
		t.Fatalf("Spec after round-trip:\n got %#v\nwant %#v", e2.Spec, e1.Spec)
	}
}
