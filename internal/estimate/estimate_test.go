package estimate

import "testing"

func TestFor(t *testing.T) {
	tests := []struct {
		name string
		vm   VM
		// total reserved and maximum, CPU in millicores and memory in MiB
		reservedCPU, reservedMem, maxCPU, maxMem int
		vmReservedMem, vmMaxCPU                  int
		volumesMiB, nodeLocalMiB                 int
	}{
		{
			name: "the lab defaults (nothing set): 1 vCPU, 512 MiB, 1 GiB",
			vm:   VM{},
			// 100m + 250m + 500m; (512+128) + 256 + 1024
			reservedCPU: 850, reservedMem: 1920,
			// 1000m + 500m + 1000m; (512+128) + 512 + 2048
			maxCPU: 2500, maxMem: 3200,
			vmReservedMem: 640, vmMaxCPU: 1000,
			// vm-data 1 GiB + vscode-data 5 GiB; vm-kernels 2 GiB + shared-config 10 MiB
			volumesMiB: 6144, nodeLocalMiB: 2058,
		},
		{
			name:        "the vm lab as sized today: 1 vCPU, 512 MiB, 1 GiB",
			vm:          VM{VCPUs: 1, MemoryMiB: 512, DiskGiB: 1},
			reservedCPU: 850, reservedMem: 1920, maxCPU: 2500, maxMem: 3200,
			vmReservedMem: 640, vmMaxCPU: 1000,
			volumesMiB: 6144, nodeLocalMiB: 2058,
		},
		{
			name: "the k8s lab: 2 vCPUs, 2048 MiB, 4 GiB",
			vm:   VM{VCPUs: 2, MemoryMiB: 2048, DiskGiB: 4},
			// the VM's CPU request doesn't grow with its vCPUs; its limit does
			reservedCPU: 850, reservedMem: 2176 + 256 + 1024,
			maxCPU: 2000 + 500 + 1000, maxMem: 2176 + 512 + 2048,
			vmReservedMem: 2176, vmMaxCPU: 2000,
			volumesMiB: 4096 + 5120, nodeLocalMiB: 8192 + 10,
		},
		{
			name:        "the largest lab: 8 vCPUs, 16384 MiB, 100 GiB",
			vm:          VM{VCPUs: 8, MemoryMiB: 16384, DiskGiB: 100},
			reservedCPU: 850, reservedMem: 16512 + 256 + 1024,
			maxCPU: 8000 + 500 + 1000, maxMem: 16512 + 512 + 2048,
			vmReservedMem: 16512, vmMaxCPU: 8000,
			volumesMiB: 102400 + 5120, nodeLocalMiB: 204800 + 10,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := For(tt.vm, "")
			if err != nil {
				t.Fatalf("For: %v", err)
			}
			if e.Persistence != PersistenceNone {
				t.Errorf("persistence = %q, want none", e.Persistence)
			}
			if got := e.Total.Reserved; got.CPUMillicores != tt.reservedCPU || got.MemoryMiB != tt.reservedMem {
				t.Errorf("total reserved = %+v, want %dm / %d MiB", got, tt.reservedCPU, tt.reservedMem)
			}
			if got := e.Total.Maximum; got.CPUMillicores != tt.maxCPU || got.MemoryMiB != tt.maxMem {
				t.Errorf("total maximum = %+v, want %dm / %d MiB", got, tt.maxCPU, tt.maxMem)
			}
			if len(e.Containers) != 3 || e.Containers[0].Name != "firecracker-vm" {
				t.Fatalf("containers = %+v", e.Containers)
			}
			vm := e.Containers[0]
			if vm.Reserved.MemoryMiB != tt.vmReservedMem || vm.Maximum.MemoryMiB != tt.vmReservedMem {
				t.Errorf("VM container memory reserved/maximum = %d/%d, want %d for both", vm.Reserved.MemoryMiB, vm.Maximum.MemoryMiB, tt.vmReservedMem)
			}
			if vm.Reserved.CPUMillicores != 100 || vm.Maximum.CPUMillicores != tt.vmMaxCPU {
				t.Errorf("VM container CPU reserved/maximum = %d/%d, want 100/%d", vm.Reserved.CPUMillicores, vm.Maximum.CPUMillicores, tt.vmMaxCPU)
			}
			if e.Storage.VolumesMiB != tt.volumesMiB || e.Storage.NodeLocalLimitMiB != tt.nodeLocalMiB {
				t.Errorf("storage volumes/node-local = %d/%d MiB, want %d/%d", e.Storage.VolumesMiB, e.Storage.NodeLocalLimitMiB, tt.volumesMiB, tt.nodeLocalMiB)
			}
			if e.Storage.AfterStopMiB != 0 {
				t.Errorf("a non-persistent VM keeps %d MiB after it stops, want 0", e.Storage.AfterStopMiB)
			}
			if e.Devices["dozlab.io/kvm"] != 1 || e.Devices["dozlab.io/tun"] != 1 {
				t.Errorf("devices = %v", e.Devices)
			}
		})
	}
}

func TestFor_FillsInTheDefaults(t *testing.T) {
	e, err := For(VM{MemoryMiB: 1024}, PersistenceNone)
	if err != nil {
		t.Fatal(err)
	}
	if e.VM != (VM{VCPUs: 1, MemoryMiB: 1024, DiskGiB: 1}) {
		t.Errorf("vm = %+v, want the set memory with the default vCPUs and disk", e.VM)
	}
}

func TestFor_Refuses(t *testing.T) {
	if _, err := For(VM{}, "files"); err == nil {
		t.Error(`persistence "files" was accepted; only "none" is available`)
	}
	if _, err := For(VM{}, "forever"); err == nil {
		t.Error("an unknown persistence was accepted")
	}
	if _, err := For(VM{VCPUs: -1}, ""); err == nil {
		t.Error("a negative VM size was accepted")
	}
}
