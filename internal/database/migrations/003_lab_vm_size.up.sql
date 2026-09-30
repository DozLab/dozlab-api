-- Each lab sets the size of its VMs, and every session of the lab gets that size (owner
-- decision, docs/decision.md "Keeping resources to a minimum"): nobody picks CPU, memory or disk
-- when creating a VM. The defaults are the smallest baseline that works for the vm lab; a lab
-- type whose tools need more (k8s: 2 vCPUs, 2048 MiB, 4 GiB) sets more. CPU and memory limits
-- match the controller's (dozlab-controller resource_builder.go).
ALTER TABLE labs ADD COLUMN IF NOT EXISTS vm_vcpus INTEGER NOT NULL DEFAULT 1
    CONSTRAINT labs_vm_vcpus_check CHECK (vm_vcpus BETWEEN 1 AND 8);
ALTER TABLE labs ADD COLUMN IF NOT EXISTS vm_memory_mib INTEGER NOT NULL DEFAULT 512
    CONSTRAINT labs_vm_memory_mib_check CHECK (vm_memory_mib BETWEEN 256 AND 16384);
ALTER TABLE labs ADD COLUMN IF NOT EXISTS vm_disk_gib INTEGER NOT NULL DEFAULT 1
    CONSTRAINT labs_vm_disk_gib_check CHECK (vm_disk_gib BETWEEN 1 AND 100);
