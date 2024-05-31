package util

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func Test_DeleteSliceFunc(t *testing.T) {
	var tests = []struct {
		name    string
		slice   []string
		match   []string
		noMatch []string
		want    []string
	}{
		{
			name:  "Example 0, Macth",
			slice: []string{"test1", "test2", "test3"},
			match: []string{"test1", "test2"},
			want:  []string{"test1", "test2"},
		},
		{
			name:  "Example 1, Macth",
			slice: []string{"test1", "test2", "test3", "test3"},
			match: []string{"test1", "test3"},
			want:  []string{"test1", "test3", "test3"},
		},
		{
			name:    "Example 2, No Macth",
			slice:   []string{"test1", "test2", "test3", "test3"},
			noMatch: []string{"test4"},
			want:    []string{"test1", "test2", "test3", "test3"},
		},
		{
			name:    "Example 3, No Match",
			slice:   []string{"test1", "test2", "test3", "test1", "test3"},
			noMatch: []string{"test1", "test2"},
			want:    []string{"test3", "test3"},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			res := DeleteSliceFunc(test.slice, func(s string) bool {
				if len(test.match) > 0 {
					return ContainsString(test.match, s)
				} else {
					return !ContainsString(test.noMatch, s)
				}
			})
			assert.Equal(t, test.want, res)
		})
	}
}

func Test_CheckResourcesInSlice(t *testing.T) {
	var tests = []struct {
		name      string
		resources map[v1.ResourceName]resource.Quantity
		slice     []string
		ignore    []string
		want      bool
	}{
		{
			name: "Example 0",
			resources: map[v1.ResourceName]resource.Quantity{
				"nvidia.com/gpu": resource.MustParse("1"),
			},
			slice:  []string{"nvidia.com/gpu"},
			ignore: nil,
			want:   true,
		},
		{
			name: "Example 1",
			resources: map[v1.ResourceName]resource.Quantity{
				"nvidia.com/gpu": resource.MustParse("0"),
			},
			slice:  []string{"nvidia.com/gpu"},
			ignore: nil,
			want:   false,
		},
		{
			name: "Example 2",
			resources: map[v1.ResourceName]resource.Quantity{
				"nvidia.com/gpu": resource.MustParse("1"),
				"xxxxxx":         resource.MustParse("1"),
			},
			slice:  []string{"nvidia.com/gpu"},
			ignore: nil,
			want:   false,
		},
		{
			name: "Example 3",
			resources: map[v1.ResourceName]resource.Quantity{
				"volcano.sh/vgpu-number": resource.MustParse("1"),
			},
			slice:  []string{"volcano.sh/vgpu-number"},
			ignore: []string{"volcano.sh/vgpu-memory", "volcano.sh/vgpu-cores", "volcano.sh/vgpu-memory-percentage"},
			want:   true,
		},
		{
			name: "Example 4",
			resources: map[v1.ResourceName]resource.Quantity{
				"volcano.sh/vgpu-number": resource.MustParse("1"),
				"volcano.sh/vgpu-memory": resource.MustParse("1000"),
			},
			slice:  []string{"volcano.sh/vgpu-number"},
			ignore: []string{"volcano.sh/vgpu-memory", "volcano.sh/vgpu-cores", "volcano.sh/vgpu-memory-percentage"},
			want:   true,
		},
		{
			name: "Example 5",
			resources: map[v1.ResourceName]resource.Quantity{
				"volcano.sh/vgpu-number": resource.MustParse("1"),
				"volcano.sh/vgpu-cores":  resource.MustParse("50"),
				"volcano.sh/vgpu-memory": resource.MustParse("1000"),
				"xxxxxxx":                resource.MustParse("1"),
			},
			slice:  []string{"volcano.sh/vgpu-number"},
			ignore: []string{"volcano.sh/vgpu-memory", "volcano.sh/vgpu-cores", "volcano.sh/vgpu-memory-percentage"},
			want:   false,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := CheckResourcesInSlice(test.resources, test.slice, test.ignore)
			assert.Equal(t, test.want, got)
		})
	}
}
