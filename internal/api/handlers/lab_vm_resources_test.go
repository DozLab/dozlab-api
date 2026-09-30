package handlers

import (
	"testing"

	"dozlab-backend/internal/models"
)

func TestLabVMResources(t *testing.T) {
	for _, tc := range []struct {
		name string
		lab  models.Lab
		want map[string]string
	}{
		{"the lab's own size", models.Lab{VMVCPUs: 2, VMMemoryMiB: 2048, VMDiskGiB: 4},
			map[string]string{"cpu": "2", "memory": "2048Mi", "storage": "4Gi"}},
		{"unset columns use the baseline", models.Lab{},
			map[string]string{"cpu": "1", "memory": "512Mi", "storage": "1Gi"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := labVMResources(tc.lab)
			for k, want := range tc.want {
				if got[k] != want {
					t.Errorf("%s = %v, want %q", k, got[k], want)
				}
			}
			if len(got) != len(tc.want) {
				t.Errorf("resources = %v, want only %v", got, tc.want)
			}
		})
	}
}
