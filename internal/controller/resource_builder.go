package controller

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	DefaultInitrdImage    = "dozman99/dozlab-initrd:latest"
	DefaultTerminalImage  = "dozman99/dozlab-terminal:latest"
	DefaultVSCodeImage    = "codercom/code-server:latest"
	DefaultNamespace      = "default"
	FinalizerName         = "dozlab.io/finalizer"
)

// ResourceBuilder builds Kubernetes resources for lab sessions
type ResourceBuilder struct {
	session   *LabSession
	namespace string
}

// NewResourceBuilder creates a new resource builder
func NewResourceBuilder(session *LabSession) *ResourceBuilder {
	namespace := session.Namespace
	if namespace == "" {
		namespace = DefaultNamespace
	}

	return &ResourceBuilder{
		session:   session,
		namespace: namespace,
	}
}

// BuildPod constructs the multi-container pod for the lab session
func (rb *ResourceBuilder) BuildPod() *corev1.Pod {
	spec := rb.session.Spec
	podName := rb.getPodName()

	// Set default values
	resources := rb.getResourceRequirements()
	images := rb.getImages()

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      podName,
			Namespace: rb.namespace,
			Labels: map[string]string{
				"app":        "lab-environment",
				"session-id": spec.SessionID,
				"user-id":    spec.UserID,
				"managed-by": "dozlab-controller",
			},
			Annotations: map[string]string{
				"dozlab.io/session-id": spec.SessionID,
				"dozlab.io/user-id":    spec.UserID,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(rb.session, GroupVersion.WithKind("LabSession")),
			},
		},
		Spec: corev1.PodSpec{
			RestartPolicy: corev1.RestartPolicyNever,
			InitContainers: []corev1.Container{
				rb.buildInitContainer(),
			},
			Containers: []corev1.Container{
				rb.buildVMContainer(images.InitrdImage, resources),
				rb.buildTerminalContainer(images.TerminalImage),
			},
			Volumes: rb.buildVolumes(),
		},
	}

	// Add VS Code container if enabled
	if spec.Config.EnableVSCode {
		pod.Spec.Containers = append(pod.Spec.Containers, rb.buildVSCodeContainer(images.VSCodeImage))
	}

	return pod
}

// BuildService constructs the Kubernetes service for the lab session
func (rb *ResourceBuilder) BuildService() *corev1.Service {
	spec := rb.session.Spec
	serviceName := rb.getServiceName()

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: rb.namespace,
			Labels: map[string]string{
				"app":        "lab-environment",
				"session-id": spec.SessionID,
				"user-id":    spec.UserID,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(rb.session, GroupVersion.WithKind("LabSession")),
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"session-id": spec.SessionID,
			},
			Ports: []corev1.ServicePort{
				{
					Name:       "terminal",
					Port:       8081,
					TargetPort: intstr.FromInt(8081),
					Protocol:   corev1.ProtocolTCP,
				},
				{
					Name:       "ssh",
					Port:       22,
					TargetPort: intstr.FromInt(22),
					Protocol:   corev1.ProtocolTCP,
				},
			},
		},
	}

	// Add VS Code port if enabled
	if spec.Config.EnableVSCode {
		service.Spec.Ports = append(service.Spec.Ports, corev1.ServicePort{
			Name:       "vscode",
			Port:       8080,
			TargetPort: intstr.FromInt(8080),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	return service
}

// BuildSecret constructs the secret for session credentials
func (rb *ResourceBuilder) BuildSecret() *corev1.Secret {
	spec := rb.session.Spec
	secretName := rb.getSecretName()

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: rb.namespace,
			Labels: map[string]string{
				"app":        "lab-environment",
				"session-id": spec.SessionID,
			},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(rb.session, GroupVersion.WithKind("LabSession")),
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"vscode-password": spec.Config.VSCodePassword,
		},
	}

	return secret
}

// BuildPVC constructs persistent volume claims for the session
func (rb *ResourceBuilder) BuildPVCs() []*corev1.PersistentVolumeClaim {
	spec := rb.session.Spec
	storageSize := "10Gi"
	if spec.Resources.Storage != "" {
		storageSize = spec.Resources.Storage
	}

	pvcs := []*corev1.PersistentVolumeClaim{
		// VM data PVC
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-vm-data", rb.getPodName()),
				Namespace: rb.namespace,
				Labels: map[string]string{
					"app":        "lab-environment",
					"session-id": spec.SessionID,
					"volume-type": "vm-data",
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(rb.session, GroupVersion.WithKind("LabSession")),
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse(storageSize),
					},
				},
			},
		},
	}

	// Add VS Code data PVC if enabled
	if spec.Config.EnableVSCode {
		pvcs = append(pvcs, &corev1.PersistentVolumeClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-vscode-data", rb.getPodName()),
				Namespace: rb.namespace,
				Labels: map[string]string{
					"app":        "lab-environment",
					"session-id": spec.SessionID,
					"volume-type": "vscode-data",
				},
				OwnerReferences: []metav1.OwnerReference{
					*metav1.NewControllerRef(rb.session, GroupVersion.WithKind("LabSession")),
				},
			},
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{
					corev1.ReadWriteOnce,
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("5Gi"),
					},
				},
			},
		})
	}

	return pvcs
}

// buildInitContainer creates the init container for network setup
func (rb *ResourceBuilder) buildInitContainer() corev1.Container {
	return corev1.Container{
		Name:  "init-network",
		Image: "busybox:1.36",
		Command: []string{
			"sh",
			"-c",
			`
POD_IP=$(hostname -i)
# Calculate VM IP by incrementing last octet by 1
VM_IP=$(echo $POD_IP | awk -F. '{print $1"."$2"."$3"."$4+1}')
echo "POD_IP=$POD_IP" > /shared/network-config/env
echo "VM_IP=$VM_IP" >> /shared/network-config/env
echo "Network configuration initialized: POD=$POD_IP, VM=$VM_IP"
			`,
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "shared-config",
				MountPath: "/shared/network-config",
			},
		},
	}
}

// buildVMContainer creates the main VM container
func (rb *ResourceBuilder) buildVMContainer(image string, resources corev1.ResourceRequirements) corev1.Container {
	container := corev1.Container{
		Name:  "vm",
		Image: image,
		Env: []corev1.EnvVar{
			{
				Name:  "SESSION_ID",
				Value: rb.session.Spec.SessionID,
			},
			{
				Name:  "USER_ID",
				Value: rb.session.Spec.UserID,
			},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "ssh",
				ContainerPort: 22,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		Resources: resources,
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "shared-config",
				MountPath: "/shared/network-config",
				ReadOnly:  true,
			},
			{
				Name:      "vm-data",
				MountPath: "/vm-data",
			},
		},
		SecurityContext: &corev1.SecurityContext{
			Privileged: boolPtr(true), // Required for VM operations
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(22),
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    3,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				TCPSocket: &corev1.TCPSocketAction{
					Port: intstr.FromInt(22),
				},
			},
			InitialDelaySeconds: 15,
			PeriodSeconds:       5,
			TimeoutSeconds:      3,
			FailureThreshold:    3,
		},
	}

	// Add custom rootfs URL if provided
	if rb.session.Spec.RootfsURL != "" {
		container.Env = append(container.Env, corev1.EnvVar{
			Name:  "ROOTFS_URL",
			Value: rb.session.Spec.RootfsURL,
		})
	}

	return container
}

// buildTerminalContainer creates the terminal sidecar container
func (rb *ResourceBuilder) buildTerminalContainer(image string) corev1.Container {
	return corev1.Container{
		Name:  "terminal",
		Image: image,
		Env: []corev1.EnvVar{
			{
				Name:  "SESSION_ID",
				Value: rb.session.Spec.SessionID,
			},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "terminal",
				ContainerPort: 8081,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "shared-config",
				MountPath: "/shared/network-config",
				ReadOnly:  true,
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health",
					Port: intstr.FromInt(8081),
				},
			},
			InitialDelaySeconds: 10,
			PeriodSeconds:       10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/health",
					Port: intstr.FromInt(8081),
				},
			},
			InitialDelaySeconds: 5,
			PeriodSeconds:       5,
		},
	}
}

// buildVSCodeContainer creates the VS Code sidecar container
func (rb *ResourceBuilder) buildVSCodeContainer(image string) corev1.Container {
	return corev1.Container{
		Name:  "vscode",
		Image: image,
		Args: []string{
			"--bind-addr",
			"0.0.0.0:8080",
			"--auth",
			"password",
		},
		Env: []corev1.EnvVar{
			{
				Name: "PASSWORD",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: rb.getSecretName(),
						},
						Key: "vscode-password",
					},
				},
			},
			{
				Name:  "SESSION_ID",
				Value: rb.session.Spec.SessionID,
			},
		},
		Ports: []corev1.ContainerPort{
			{
				Name:          "http",
				ContainerPort: 8080,
				Protocol:      corev1.ProtocolTCP,
			},
		},
		VolumeMounts: []corev1.VolumeMount{
			{
				Name:      "vm-data",
				MountPath: "/home/coder/project",
			},
			{
				Name:      "vscode-data",
				MountPath: "/home/coder/.local/share/code-server",
			},
		},
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("200m"),
				corev1.ResourceMemory: resource.MustParse("512Mi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1000m"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		LivenessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 30,
			PeriodSeconds:       10,
		},
		ReadinessProbe: &corev1.Probe{
			ProbeHandler: corev1.ProbeHandler{
				HTTPGet: &corev1.HTTPGetAction{
					Path: "/healthz",
					Port: intstr.FromInt(8080),
				},
			},
			InitialDelaySeconds: 15,
			PeriodSeconds:       5,
		},
	}
}

// buildVolumes creates the volume specifications
func (rb *ResourceBuilder) buildVolumes() []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name: "shared-config",
			VolumeSource: corev1.VolumeSource{
				EmptyDir: &corev1.EmptyDirVolumeSource{},
			},
		},
		{
			Name: "vm-data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: fmt.Sprintf("%s-vm-data", rb.getPodName()),
				},
			},
		},
	}

	// Add VS Code data volume if enabled
	if rb.session.Spec.Config.EnableVSCode {
		volumes = append(volumes, corev1.Volume{
			Name: "vscode-data",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: fmt.Sprintf("%s-vscode-data", rb.getPodName()),
				},
			},
		})
	}

	return volumes
}

// getResourceRequirements calculates resource requirements with proper limits
func (rb *ResourceBuilder) getResourceRequirements() corev1.ResourceRequirements {
	spec := rb.session.Spec

	// Default values
	cpuRequest := "500m"
	cpuLimit := "2"
	memRequest := "1Gi"
	memLimit := "4Gi"

	// Parse custom values if provided
	if spec.Resources.CPU != "" {
		cpuLimit = spec.Resources.CPU
		// Calculate request as 25% of limit
		if strings.HasSuffix(cpuLimit, "m") {
			cpuRequest = cpuLimit // Use same for millicores
		} else {
			cpuRequest = fmt.Sprintf("%sm", cpuLimit) + "00" // Convert cores to millicores
		}
	}

	if spec.Resources.Memory != "" {
		memLimit = spec.Resources.Memory
		// Calculate request as 25% of limit
		memRequest = memLimit
	}

	// Enforce maximum limits (8 CPU, 16Gi RAM)
	maxCPU := resource.MustParse("8")
	maxMem := resource.MustParse("16Gi")

	cpuLimitQty := resource.MustParse(cpuLimit)
	memLimitQty := resource.MustParse(memLimit)

	if cpuLimitQty.Cmp(maxCPU) > 0 {
		cpuLimitQty = maxCPU
	}
	if memLimitQty.Cmp(maxMem) > 0 {
		memLimitQty = maxMem
	}

	// Calculate requests as 25% of limits
	cpuRequestQty := cpuLimitQty.DeepCopy()
	cpuRequestQty.Set(cpuRequestQty.Value() / 4)

	memRequestQty := memLimitQty.DeepCopy()
	memRequestQty.Set(memRequestQty.Value() / 4)

	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{
			corev1.ResourceCPU:    cpuRequestQty,
			corev1.ResourceMemory: memRequestQty,
		},
		Limits: corev1.ResourceList{
			corev1.ResourceCPU:    cpuLimitQty,
			corev1.ResourceMemory: memLimitQty,
		},
	}
}

// getImages returns the container images to use
func (rb *ResourceBuilder) getImages() struct {
	InitrdImage   string
	TerminalImage string
	VSCodeImage   string
} {
	images := struct {
		InitrdImage   string
		TerminalImage string
		VSCodeImage   string
	}{
		InitrdImage:   DefaultInitrdImage,
		TerminalImage: DefaultTerminalImage,
		VSCodeImage:   DefaultVSCodeImage,
	}

	if rb.session.Spec.CustomImages.InitrdImage != "" {
		images.InitrdImage = rb.session.Spec.CustomImages.InitrdImage
	}
	if rb.session.Spec.CustomImages.TerminalImage != "" {
		images.TerminalImage = rb.session.Spec.CustomImages.TerminalImage
	}
	if rb.session.Spec.CustomImages.VSCodeImage != "" {
		images.VSCodeImage = rb.session.Spec.CustomImages.VSCodeImage
	}

	return images
}

// Helper functions for resource naming

func (rb *ResourceBuilder) getPodName() string {
	return fmt.Sprintf("lab-session-%s", rb.session.Spec.SessionID)
}

func (rb *ResourceBuilder) getServiceName() string {
	return fmt.Sprintf("lab-service-%s", rb.session.Spec.SessionID)
}

func (rb *ResourceBuilder) getSecretName() string {
	return fmt.Sprintf("lab-session-%s-secrets", rb.session.Spec.SessionID)
}

func boolPtr(b bool) *bool {
	return &b
}
