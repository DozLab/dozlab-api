package hostcheck

import (
	"reflect"
	"strings"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

// Expected values come from running check_host_requirements in
// .agent/local_llm_mcp.py on the same inputs.
func TestEvaluateMatchesReference(t *testing.T) {
	small := Requests{CPU: "500m", Memory: "2Gi", Storage: "10Gi"}
	bigHost := Host{CPUCores: 8, Memory: "32Gi", FreeStorage: "200Gi", KVM: boolPtr(true)}
	noKVMHost := bigHost
	noKVMHost.KVM = boolPtr(false)
	noStorageHost := bigHost
	noStorageHost.FreeStorage = ""
	defaults := Options{Sessions: 1, RequireKVM: true, Headroom: DefaultHeadroom}

	tests := []struct {
		name        string
		req         Requests
		host        Host
		opts        Options
		wantOK      bool
		wantCPU     Check
		wantMem     Check
		wantDisk    Check
		wantKVM     *KVMCheck
		wantUnknown []string
		wantMax     int
	}{
		{
			name: "fits", req: small, host: bigHost, opts: defaults,
			wantOK:      true,
			wantCPU:     Check{0.5, 6.4, true},
			wantMem:     Check{2, 25.6, true},
			wantDisk:    Check{10, 160, true},
			wantKVM:     &KVMCheck{true, boolPtr(true), true},
			wantUnknown: []string{},
			wantMax:     12,
		},
		{
			name:        "ten sessions",
			req:         Requests{CPU: "1", Memory: "4Gi", Storage: "20Gi"},
			host:        bigHost,
			opts:        Options{Sessions: 10, RequireKVM: true, Headroom: DefaultHeadroom},
			wantOK:      false,
			wantCPU:     Check{10, 6.4, false},
			wantMem:     Check{40, 25.6, false},
			wantDisk:    Check{200, 160, false},
			wantKVM:     &KVMCheck{true, boolPtr(true), true},
			wantUnknown: []string{},
			wantMax:     6,
		},
		{
			name: "no kvm", req: small, host: noKVMHost, opts: defaults,
			wantOK:      false,
			wantCPU:     Check{0.5, 6.4, true},
			wantMem:     Check{2, 25.6, true},
			wantDisk:    Check{10, 160, true},
			wantKVM:     &KVMCheck{true, boolPtr(false), false},
			wantUnknown: []string{},
			wantMax:     12,
		},
		{
			name: "kvm not required", req: small, host: noKVMHost,
			opts:        Options{Sessions: 1, RequireKVM: false, Headroom: DefaultHeadroom},
			wantOK:      true,
			wantCPU:     Check{0.5, 6.4, true},
			wantMem:     Check{2, 25.6, true},
			wantDisk:    Check{10, 160, true},
			wantUnknown: []string{},
			wantMax:     12,
		},
		{
			name: "unknown storage", req: small, host: noStorageHost, opts: defaults,
			wantOK:      false,
			wantCPU:     Check{0.5, 6.4, true},
			wantMem:     Check{2, 25.6, true},
			wantDisk:    Check{10, 0, false},
			wantKVM:     &KVMCheck{true, boolPtr(true), true},
			wantUnknown: []string{"free_storage"},
			wantMax:     0,
		},
		{
			name:        "no headroom, exact fit",
			req:         Requests{CPU: "2", Memory: "8Gi", Storage: "50Gi"},
			host:        Host{CPUCores: 4, Memory: "16Gi", FreeStorage: "100Gi", KVM: boolPtr(true)},
			opts:        Options{Sessions: 2, RequireKVM: true, Headroom: 0},
			wantOK:      true,
			wantCPU:     Check{4, 4, true},
			wantMem:     Check{16, 16, true},
			wantDisk:    Check{100, 100, true},
			wantKVM:     &KVMCheck{true, boolPtr(true), true},
			wantUnknown: []string{},
			wantMax:     2,
		},
		{
			name:        "decimal units",
			req:         Requests{CPU: "250m", Memory: "512Mi", Storage: "5G"},
			host:        Host{CPUCores: 2, Memory: "4G", FreeStorage: "50G", KVM: boolPtr(true)},
			opts:        defaults,
			wantOK:      true,
			wantCPU:     Check{0.25, 1.6, true},
			wantMem:     Check{0.5, 2.98, true},
			wantDisk:    Check{4.66, 37.25, true},
			wantKVM:     &KVMCheck{true, boolPtr(true), true},
			wantUnknown: []string{},
			wantMax:     5,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Evaluate(tt.req, tt.host, tt.opts)
			if err != nil {
				t.Fatalf("Evaluate: %v", err)
			}
			if got.OK != tt.wantOK {
				t.Errorf("OK = %v, want %v", got.OK, tt.wantOK)
			}
			if got.CPU != tt.wantCPU {
				t.Errorf("CPU = %+v, want %+v", got.CPU, tt.wantCPU)
			}
			if got.MemoryGiB != tt.wantMem {
				t.Errorf("MemoryGiB = %+v, want %+v", got.MemoryGiB, tt.wantMem)
			}
			if got.StorageGiB != tt.wantDisk {
				t.Errorf("StorageGiB = %+v, want %+v", got.StorageGiB, tt.wantDisk)
			}
			if !reflect.DeepEqual(got.KVM, tt.wantKVM) {
				t.Errorf("KVM = %+v, want %+v", got.KVM, tt.wantKVM)
			}
			if !reflect.DeepEqual(got.UnknownFields, tt.wantUnknown) {
				t.Errorf("UnknownFields = %v, want %v", got.UnknownFields, tt.wantUnknown)
			}
			if got.MaxSessions != tt.wantMax {
				t.Errorf("MaxSessions = %d, want %d", got.MaxSessions, tt.wantMax)
			}
		})
	}
}

func TestEvaluateNoRequests(t *testing.T) {
	host := Host{CPUCores: 4, Memory: "8Gi", FreeStorage: "20Gi", KVM: boolPtr(true)}
	got, err := Evaluate(Requests{}, host, Options{Sessions: 1, RequireKVM: true, Headroom: DefaultHeadroom})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if !got.OK || got.MaxSessions != unlimitedSessions {
		t.Errorf("OK=%v MaxSessions=%d, want true and %d", got.OK, got.MaxSessions, unlimitedSessions)
	}
}

func TestEvaluateErrors(t *testing.T) {
	good := Requests{CPU: "500m", Memory: "2Gi", Storage: "10Gi"}
	host := Host{CPUCores: 8, Memory: "32Gi", FreeStorage: "200Gi", KVM: boolPtr(true)}
	defaults := Options{Sessions: 1, RequireKVM: true, Headroom: DefaultHeadroom}

	tests := []struct {
		name    string
		req     Requests
		host    Host
		opts    Options
		wantErr string
	}{
		{"zero sessions", good, host, Options{Sessions: 0, Headroom: DefaultHeadroom}, "sessions"},
		{"negative headroom", good, host, Options{Sessions: 1, Headroom: -0.1}, "headroom"},
		{"headroom of 1", good, host, Options{Sessions: 1, Headroom: 1}, "headroom"},
		{"negative cores", good, Host{CPUCores: -1}, defaults, "cpu_cores"},
		{"bad cpu", Requests{CPU: "fast"}, host, defaults, "requests.cpu"},
		{"bad memory", Requests{Memory: "8 GB"}, host, defaults, "requests.memory"},
		{"negative storage", Requests{Storage: "-1Gi"}, host, defaults, "requests.storage"},
		{"bad host memory", good, Host{CPUCores: 8, Memory: "lots"}, defaults, "host.memory"},
		{"bad host storage", good, Host{CPUCores: 8, FreeStorage: "?"}, defaults, "host.free_storage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Evaluate(tt.req, tt.host, tt.opts)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("err = %v, want it to mention %q", err, tt.wantErr)
			}
		})
	}
}
