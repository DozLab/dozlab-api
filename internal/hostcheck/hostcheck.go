// Package hostcheck decides whether a host or node can run a number of lab
// sessions, given a LabSession's resource requests. The arithmetic mirrors
// check_host_requirements in the workspace's .agent/local_llm_mcp.py.
package hostcheck

import (
	"errors"
	"fmt"
	"math"

	"k8s.io/apimachinery/pkg/api/resource"
)

// DefaultHeadroom is the fraction of the host reserved for the OS and kubelet.
const DefaultHeadroom = 0.2

// unlimitedSessions is reported for a dimension that requests nothing.
const unlimitedSessions = 1_000_000

const gib = 1 << 30

// Requests is a LabSession's resource requests, as in the controller's
// LabSessionResourceRequests: Kubernetes quantities such as "500m", "2Gi".
type Requests struct {
	CPU     string `json:"cpu,omitempty"`
	Memory  string `json:"memory,omitempty"`
	Storage string `json:"storage,omitempty"`
}

// Host describes the machine or node. Empty/zero fields are unknown.
type Host struct {
	CPUCores    float64 `json:"cpu_cores"`
	Memory      string  `json:"memory"`
	FreeStorage string  `json:"free_storage"`
	KVM         *bool   `json:"kvm"`
}

// Options tune the check. Zero values mean "use the default".
type Options struct {
	Sessions   int
	RequireKVM bool
	Headroom   float64
}

// Check is one resource comparison. Need and Usable are CPU cores, or GiB
// for memory and storage, rounded to 2 decimals.
type Check struct {
	Need   float64 `json:"need"`
	Usable float64 `json:"usable"`
	Pass   bool    `json:"pass"`
}

// KVMCheck reports whether hardware virtualisation is available.
type KVMCheck struct {
	Need bool  `json:"need"`
	Have *bool `json:"have"`
	Pass bool  `json:"pass"`
}

// Result is the outcome of a host check. OK is true only when every check
// passes and no host field is unknown.
type Result struct {
	OK            bool      `json:"ok"`
	CPU           Check     `json:"cpu"`
	MemoryGiB     Check     `json:"memory_gib"`
	StorageGiB    Check     `json:"storage_gib"`
	KVM           *KVMCheck `json:"kvm,omitempty"`
	UnknownFields []string  `json:"unknown_fields"`
	MaxSessions   int       `json:"max_sessions"`
}

// Evaluate checks whether host can run opts.Sessions sessions with the given requests.
func Evaluate(req Requests, host Host, opts Options) (Result, error) {
	if opts.Sessions < 1 {
		return Result{}, errors.New("sessions must be at least 1")
	}
	if opts.Headroom < 0 || opts.Headroom >= 1 {
		return Result{}, errors.New("headroom must be in [0, 1)")
	}
	if host.CPUCores < 0 {
		return Result{}, errors.New("cpu_cores must not be negative")
	}

	perCPU, err := cores(req.CPU, "requests.cpu")
	if err != nil {
		return Result{}, err
	}
	perMem, err := bytes(req.Memory, "requests.memory")
	if err != nil {
		return Result{}, err
	}
	perDisk, err := bytes(req.Storage, "requests.storage")
	if err != nil {
		return Result{}, err
	}
	hostMem, err := bytes(host.Memory, "host.memory")
	if err != nil {
		return Result{}, err
	}
	hostDisk, err := bytes(host.FreeStorage, "host.free_storage")
	if err != nil {
		return Result{}, err
	}

	n := float64(opts.Sessions)
	needCPU, needMem, needDisk := perCPU*n, perMem*n, perDisk*n
	usable := 1 - opts.Headroom
	haveCPU, haveMem, haveDisk := host.CPUCores*usable, hostMem*usable, hostDisk*usable

	res := Result{
		CPU:           Check{Need: round2(needCPU), Usable: round2(haveCPU), Pass: haveCPU >= needCPU},
		MemoryGiB:     Check{Need: round2(needMem / gib), Usable: round2(haveMem / gib), Pass: haveMem >= needMem},
		StorageGiB:    Check{Need: round2(needDisk / gib), Usable: round2(haveDisk / gib), Pass: haveDisk >= needDisk},
		UnknownFields: []string{},
		MaxSessions: min(
			fits(haveCPU, perCPU),
			fits(haveMem, perMem),
			fits(haveDisk, perDisk),
		),
	}
	if opts.RequireKVM {
		res.KVM = &KVMCheck{Need: true, Have: host.KVM, Pass: host.KVM != nil && *host.KVM}
	}
	if host.CPUCores == 0 {
		res.UnknownFields = append(res.UnknownFields, "cpu_cores")
	}
	if host.Memory == "" {
		res.UnknownFields = append(res.UnknownFields, "memory")
	}
	if host.FreeStorage == "" {
		res.UnknownFields = append(res.UnknownFields, "free_storage")
	}

	res.OK = res.CPU.Pass && res.MemoryGiB.Pass && res.StorageGiB.Pass &&
		(res.KVM == nil || res.KVM.Pass) && len(res.UnknownFields) == 0
	return res, nil
}

// fits is how many sessions needing per fit into have.
func fits(have, per float64) int {
	if per == 0 {
		return unlimitedSessions
	}
	return int(math.Floor(have / per))
}

func cores(q, field string) (float64, error) {
	if q == "" {
		return 0, nil
	}
	v, err := parse(q, field)
	if err != nil {
		return 0, err
	}
	return float64(v.MilliValue()) / 1000, nil
}

func bytes(q, field string) (float64, error) {
	if q == "" {
		return 0, nil
	}
	v, err := parse(q, field)
	if err != nil {
		return 0, err
	}
	return v.AsApproximateFloat64(), nil
}

func parse(q, field string) (resource.Quantity, error) {
	v, err := resource.ParseQuantity(q)
	if err != nil {
		return resource.Quantity{}, fmt.Errorf("%s: invalid quantity %q", field, q)
	}
	if v.Sign() < 0 {
		return resource.Quantity{}, fmt.Errorf("%s: must not be negative", field)
	}
	return v, nil
}

func round2(f float64) float64 {
	return math.Round(f*100) / 100
}
