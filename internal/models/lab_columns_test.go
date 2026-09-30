package models

import (
	"sync"
	"testing"

	"gorm.io/gorm/schema"
)

// GORM derives column names from field names and ignores db tags; a field like VMVCPUs would
// silently map to vm_v_cpus and always read 0. Check the columns the migrations create.
func TestLabColumnNames(t *testing.T) {
	s, err := schema.Parse(&Lab{}, &sync.Map{}, schema.NamingStrategy{})
	if err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{
		"InitImage":   "init_image",
		"VMVCPUs":     "vm_vcpus",
		"VMMemoryMiB": "vm_memory_mib",
		"VMDiskGiB":   "vm_disk_gib",
	} {
		f := s.LookUpField(field)
		if f == nil {
			t.Errorf("no field %s", field)
			continue
		}
		if f.DBName != want {
			t.Errorf("%s maps to column %q, want %q", field, f.DBName, want)
		}
	}
}
