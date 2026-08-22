package catalog_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ditwrd/pavedway/internal/catalog"
)

func readFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/catalog-info.yaml")
	require.NoError(t, err, "read fixture")
	return data
}

// Ticket #22 AC3: a real, unmodified Backstage catalog-info.yaml loads into
// pavedway without errors, with its fields intact.
func TestLoad_BackstageCatalogInfo(t *testing.T) {
	e, err := catalog.Load(readFixture(t))
	require.NoError(t, err, "Load() error")

	require.Equal(t, "Component", e.Kind, "Kind")
	require.Equal(t, "artist-web", e.Name, "Name")
	require.Equal(t, "default", e.Namespace, "Namespace (default when metadata.namespace is absent)")

	got, _ := e.Metadata["description"].(string)
	require.Equal(t, "The artist web", got, "Metadata.description")

	spec := e.Spec
	for key, want := range map[string]any{
		"type":      "website",
		"lifecycle": "production",
		"owner":     "team-a",
	} {
		require.Equal(t, want, spec[key], "Spec[%q]", key)
	}
}

// Ticket #22 AC4: an annotation not recognized by pavedway is preserved on
// the entity after load, not dropped.
func TestLoad_UnknownAnnotationsPreserved(t *testing.T) {
	e, err := catalog.Load(readFixture(t))
	require.NoError(t, err, "Load() error")

	// Normalize through JSON so the assertion is independent of whether the
	// loader's yaml library yields map[string]any or map[string]string.
	raw, err := json.Marshal(e.Metadata["annotations"])
	require.NoError(t, err, "marshal annotations")

	var annotations map[string]string
	require.NoError(t, json.Unmarshal(raw, &annotations), "unmarshal annotations")

	for k, want := range map[string]string{
		"backstage.io/techdocs-ref": "dir:.", // TechDocs-specific: unknown to pavedway
		"github.com/project-slug":   "example/artist-web",
		"example.com/custom-tag":    "keep-me",
	} {
		got := annotations[k]
		require.Equal(t, want, got, "annotation %q (must survive byte-for-byte)", k)
	}
}

// Ticket #22 AC3: fields and annotations survive a YAML round-trip within
// the supported kind — serialize the loaded entity and re-load the output.
func TestRoundTrip_FieldsAndAnnotationsSurvive(t *testing.T) {
	e1, err := catalog.Load(readFixture(t))
	require.NoError(t, err, "Load() error")

	out, err := e1.ToYAML()
	require.NoError(t, err, "ToYAML() error")
	require.Contains(t, string(out), "example.com/custom-tag", "ToYAML() output dropped the unknown annotation:\n%s", out)

	e2, err := catalog.Load(out)
	require.NoError(t, err, "Load(ToYAML output) error")

	require.True(t, e2.Kind == e1.Kind && e2.Name == e1.Name && e2.Namespace == e1.Namespace, "identity after round-trip")
	require.Equal(t, "backstage.io/v1alpha1", e1.APIVersion, "APIVersion")
	require.Equal(t, e1.APIVersion, e2.APIVersion, "APIVersion after round-trip")
	require.Equal(t, e1.Metadata, e2.Metadata, "Metadata after round-trip")
	require.Equal(t, e1.Spec, e2.Spec, "Spec after round-trip")
}
