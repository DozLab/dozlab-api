// Package estimate works out what one VM of a lab reserves and stores in the cluster, so an
// instructor can see it before creating the VM (docs/decision.md, "Resources each option
// uses").
//
// The numbers are a copy of what dozlab-controller gives a lab pod. They were taken from its
// main branch at 8700ba7 (internal/controller/resource_builder.go and pod_settings.go); when
// the controller changes them, change them here too.
package estimate

import "fmt"

// From dozlab-controller internal/controller/resource_builder.go.
const (
	// buildVMContainer: the VM container reserves the VM's memory plus firecrackerOverheadMiB,
	// and little CPU (vmContainerCPURequest); its CPU limit is the VM's vCPU count.
	firecrackerOverheadMiB = 128
	vmCPURequestMilli      = 100

	// buildTerminalContainer
	terminalCPURequestMilli, terminalCPULimitMilli = 250, 500
	terminalMemRequestMiB, terminalMemLimitMiB     = 256, 512

	// The code-server container
	codeServerCPURequestMilli, codeServerCPULimitMilli = 500, 1000
	codeServerMemRequestMiB, codeServerMemLimitMiB     = 1024, 2048

	// BuildPVCs: the vscode-data claim. The vm-data claim is the session's spec.resources.storage,
	// which the API sets to the lab's disk size.
	vscodeDataMiB = 5 * 1024
	// buildVolumes: the shared-config emptyDir
	sharedConfigMiB = 10
	// vmKernelsSizeLimit: the vm-kernels emptyDir is limited to twice the disk
	vmKernelsDiskFactor = 2
)

// The lab's VM size when a field isn't set (labVMResources in the lab-session handler and the
// labs table's defaults, migration 003).
const (
	DefaultVCPUs     = 1
	DefaultMemoryMiB = 512
	DefaultDiskGiB   = 1
)

// PersistenceNone is the only session option there is today: a VM whose disk goes when it stops.
const PersistenceNone = "none"

// VM is the size of the lab's VM.
type VM struct {
	VCPUs     int `json:"vcpus"`
	MemoryMiB int `json:"memory_mib"`
	DiskGiB   int `json:"disk_gib"`
}

// Amount is CPU in thousandths of a core and memory in MiB.
type Amount struct {
	CPUMillicores int `json:"cpu_millicores"`
	MemoryMiB     int `json:"memory_mib"`
}

// Container is one container of the VM's pod. Reserved is what the cluster sets aside for it
// (its request), which is what limits how many VMs fit on a node; Maximum is its limit.
type Container struct {
	Name     string `json:"name"`
	Purpose  string `json:"purpose"`
	Reserved Amount `json:"reserved"`
	Maximum  Amount `json:"maximum"`
}

// Volume is one piece of storage the VM uses while it runs.
type Volume struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose"`
	// Kind is "volume" for a persistent volume claim, whose full size is set aside, or
	// "node-local" for temporary space on the node, where SizeMiB is the most it may use.
	Kind    string `json:"kind"`
	SizeMiB int    `json:"size_mib"`
}

// Storage is what the VM stores.
type Storage struct {
	WhileRunning []Volume `json:"while_running"`
	// VolumesMiB is the total set aside in volumes while the VM runs.
	VolumesMiB int `json:"volumes_mib"`
	// NodeLocalLimitMiB is the most temporary space the VM may use on its node.
	NodeLocalLimitMiB int `json:"node_local_limit_mib"`
	// AfterStopMiB is what is kept once the VM has stopped.
	AfterStopMiB int `json:"after_stop_mib"`
}

// Estimate is what one VM of a lab takes.
type Estimate struct {
	Persistence string      `json:"persistence"`
	VM          VM          `json:"vm"`
	Containers  []Container `json:"containers"`
	// Total is the sum over the containers: what one running VM reserves and may use at most.
	Total struct {
		Reserved Amount `json:"reserved"`
		Maximum  Amount `json:"maximum"`
	} `json:"total"`
	// Devices are the node devices the VM holds while it runs.
	Devices map[string]int `json:"devices"`
	Storage Storage        `json:"storage"`
}

// For returns the estimate for a VM of the given size. A field that is zero takes the lab
// default. persistence must be PersistenceNone or empty (which means none).
func For(vm VM, persistence string) (*Estimate, error) {
	if persistence == "" {
		persistence = PersistenceNone
	}
	if persistence != PersistenceNone {
		return nil, fmt.Errorf("persistence %q is not available; the only value is %q", persistence, PersistenceNone)
	}
	if vm.VCPUs == 0 {
		vm.VCPUs = DefaultVCPUs
	}
	if vm.MemoryMiB == 0 {
		vm.MemoryMiB = DefaultMemoryMiB
	}
	if vm.DiskGiB == 0 {
		vm.DiskGiB = DefaultDiskGiB
	}
	if vm.VCPUs < 0 || vm.MemoryMiB < 0 || vm.DiskGiB < 0 {
		return nil, fmt.Errorf("the VM size can't be negative")
	}

	vmMemory := vm.MemoryMiB + firecrackerOverheadMiB
	diskMiB := vm.DiskGiB * 1024

	e := &Estimate{
		Persistence: persistence,
		VM:          vm,
		Containers: []Container{
			{
				Name:     "firecracker-vm",
				Purpose:  "runs the VM",
				Reserved: Amount{CPUMillicores: vmCPURequestMilli, MemoryMiB: vmMemory},
				Maximum:  Amount{CPUMillicores: vm.VCPUs * 1000, MemoryMiB: vmMemory},
			},
			{
				Name:     "terminal-sidecar",
				Purpose:  "the browser terminal (SSH into the VM)",
				Reserved: Amount{CPUMillicores: terminalCPURequestMilli, MemoryMiB: terminalMemRequestMiB},
				Maximum:  Amount{CPUMillicores: terminalCPULimitMilli, MemoryMiB: terminalMemLimitMiB},
			},
			{
				Name:     "code-server",
				Purpose:  "the browser editor",
				Reserved: Amount{CPUMillicores: codeServerCPURequestMilli, MemoryMiB: codeServerMemRequestMiB},
				Maximum:  Amount{CPUMillicores: codeServerCPULimitMilli, MemoryMiB: codeServerMemLimitMiB},
			},
		},
		Devices: map[string]int{"dozlab.io/kvm": 1, "dozlab.io/tun": 1},
		Storage: Storage{
			WhileRunning: []Volume{
				{Name: "vm-kernels", Purpose: "the VM's disk", Kind: "node-local", SizeMiB: diskMiB * vmKernelsDiskFactor},
				{Name: "shared-config", Purpose: "settings shared by the containers", Kind: "node-local", SizeMiB: sharedConfigMiB},
				{Name: "vm-data", Purpose: "data volume for the VM", Kind: "volume", SizeMiB: diskMiB},
				{Name: "vscode-data", Purpose: "the editor's settings and extensions", Kind: "volume", SizeMiB: vscodeDataMiB},
			},
			// Non-persistent: the volumes belong to the session and go when it is deleted
			AfterStopMiB: 0,
		},
	}
	for _, c := range e.Containers {
		e.Total.Reserved.CPUMillicores += c.Reserved.CPUMillicores
		e.Total.Reserved.MemoryMiB += c.Reserved.MemoryMiB
		e.Total.Maximum.CPUMillicores += c.Maximum.CPUMillicores
		e.Total.Maximum.MemoryMiB += c.Maximum.MemoryMiB
	}
	for _, v := range e.Storage.WhileRunning {
		if v.Kind == "volume" {
			e.Storage.VolumesMiB += v.SizeMiB
		} else {
			e.Storage.NodeLocalLimitMiB += v.SizeMiB
		}
	}
	return e, nil
}
