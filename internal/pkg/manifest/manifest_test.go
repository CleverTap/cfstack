package manifest

import (
	"fmt"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestParse(t *testing.T) {
	testCases := map[string]struct {
		manifestFile    string
		expectedRegions int
		expectedStacks  int
		exceptedError   error
	}{
		"parse valid manifest file": {
			manifestFile:    "../../../testdata/manifest.json",
			expectedRegions: 1,
			expectedStacks:  2,
		},
		"invalid region": {
			manifestFile:  "../../../testdata/manifest-invalid-region.json",
			exceptedError: fmt.Errorf("eu-west is not a valid region"),
		},
		"missing region name": {
			manifestFile:  "../../../testdata/manifest-missing-region-name.json",
			exceptedError: fmt.Errorf("Region name is missing for %d element", 0),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			m := Manifest{}

			err := m.Parse(tc.manifestFile)

			if tc.exceptedError != nil {
				require.EqualError(t, err, tc.exceptedError.Error())
			} else {
				require.Equal(t, len(m.Regions), tc.expectedRegions)
				require.Equal(t, len(m.Regions[0].Stacks), tc.expectedStacks)
			}
		})
	}

}
