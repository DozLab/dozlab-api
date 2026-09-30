package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// The ID columns have no database default so the models work on both
// PostgreSQL and SQLite; the BeforeCreate hooks below assign them instead.

// BeforeCreate assigns a new ID if none is set.
func (u *User) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}

// BeforeCreate assigns a new ID if none is set.
func (l *Lab) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}

// BeforeCreate assigns a new ID if none is set.
func (ls *LabSpec) BeforeCreate(tx *gorm.DB) error {
	if ls.ID == uuid.Nil {
		ls.ID = uuid.New()
	}
	return nil
}

// BeforeCreate assigns a new ID if none is set.
func (s *Session) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

// BeforeCreate assigns a new ID if none is set.
func (up *UserProgress) BeforeCreate(tx *gorm.DB) error {
	if up.ID == uuid.Nil {
		up.ID = uuid.New()
	}
	return nil
}

// User represents a user in the system
type User struct {
	ID           uuid.UUID  `json:"id" db:"id" gorm:"type:uuid;primary_key"`
	Username     string     `json:"username" db:"username" gorm:"type:varchar(50);uniqueIndex;not null" binding:"required,min=3,max=50"`
	Email        string     `json:"email" db:"email" gorm:"type:varchar(255);uniqueIndex;not null" binding:"required,email"`
	PasswordHash string     `json:"-" db:"password_hash" gorm:"type:varchar(255);not null"`
	FirstName    *string    `json:"first_name,omitempty" db:"first_name" gorm:"type:varchar(100)"`
	LastName     *string    `json:"last_name,omitempty" db:"last_name" gorm:"type:varchar(100)"`
	Role         string     `json:"role" db:"role" gorm:"type:varchar(20);default:student;check:role IN ('admin','instructor','student')"`
	IsActive     bool       `json:"is_active" db:"is_active" gorm:"default:true"`
	CreatedAt    time.Time  `json:"created_at" db:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time  `json:"updated_at" db:"updated_at" gorm:"autoUpdateTime"`
	LastLoginAt  *time.Time `json:"last_login_at,omitempty" db:"last_login_at"`
}

// Lab represents a lab definition
type Lab struct {
	ID                uuid.UUID  `json:"id" db:"id" gorm:"type:uuid;primary_key"`
	Name              string     `json:"name" db:"name" gorm:"type:varchar(255);not null" binding:"required,min=3,max=255"`
	Slug              string     `json:"slug" db:"slug" gorm:"type:varchar(100);uniqueIndex;not null" binding:"required,min=3,max=100"`
	Description       *string    `json:"description,omitempty" db:"description" gorm:"type:text"`
	DifficultyLevel   string     `json:"difficulty_level" db:"difficulty_level" gorm:"type:varchar(20);default:beginner;check:difficulty_level IN ('beginner','intermediate','advanced')"`
	EstimatedDuration *int       `json:"estimated_duration,omitempty" db:"estimated_duration"` // minutes
	Category          *string    `json:"category,omitempty" db:"category" gorm:"type:varchar(50)"`
	Tags              []string   `json:"tags,omitempty" db:"tags" gorm:"type:text[]"`
	IsPublished       bool       `json:"is_published" db:"is_published" gorm:"default:false"`
	// InitImage is the lab's rootfs init image (dozlab-rootfs-manager dozlab-init-<lab>); empty
	// uses the controller's default. It becomes the LabSession's spec.customImages.initImage.
	InitImage         *string    `json:"init_image,omitempty" db:"init_image" gorm:"type:varchar(255)"`
	CreatedBy         *uuid.UUID `json:"created_by,omitempty" db:"created_by" gorm:"type:uuid;index"`
	CreatedAt         time.Time  `json:"created_at" db:"created_at" gorm:"autoCreateTime"`
	UpdatedAt         time.Time  `json:"updated_at" db:"updated_at" gorm:"autoUpdateTime"`
	Version           int        `json:"version" db:"version" gorm:"default:1"`

	// Relationships
	Creator   *User      `json:"creator,omitempty" gorm:"foreignKey:CreatedBy"`
	LabSpecs  []LabSpec  `json:"lab_specs,omitempty" gorm:"foreignKey:LabID"`
	Sessions  []Session  `json:"sessions,omitempty" gorm:"foreignKey:LabID"`
}

// LabSpec represents lab specifications with composite primary key
type LabSpec struct {
	ID                 uuid.UUID   `json:"id" db:"id" gorm:"type:uuid;uniqueIndex;not null"`
	LabID              uuid.UUID   `json:"lab_id" db:"lab_id" gorm:"type:uuid;primaryKey"`
	Version            int         `json:"version" db:"version" gorm:"primaryKey"`
	Specification      interface{} `json:"specification" db:"specification" gorm:"type:jsonb;not null"`
	KubernetesManifest interface{} `json:"kubernetes_manifest,omitempty" db:"kubernetes_manifest" gorm:"type:jsonb"`
	ValidationRules    interface{} `json:"validation_rules,omitempty" db:"validation_rules" gorm:"type:jsonb"`
	IsActive           bool        `json:"is_active" db:"is_active" gorm:"default:true"`
	CreatedAt          time.Time   `json:"created_at" db:"created_at" gorm:"autoCreateTime"`

	// Relationships
	Lab      Lab       `json:"lab,omitempty" gorm:"foreignKey:LabID"`
	Sessions []Session `json:"sessions,omitempty" gorm:"foreignKey:LabSpecID;references:ID"`
}

// Session represents an active lab session
type Session struct {
	ID           uuid.UUID   `json:"id" db:"id" gorm:"type:uuid;primary_key"`
	UserID       uuid.UUID   `json:"user_id" db:"user_id" gorm:"type:uuid;not null;index"`
	LabID        uuid.UUID   `json:"lab_id" db:"lab_id" gorm:"type:uuid;not null;index"`
	LabSpecID    *uuid.UUID  `json:"lab_spec_id,omitempty" db:"lab_spec_id" gorm:"type:uuid;index"`
	Status       string      `json:"status" db:"status" gorm:"type:varchar(20);default:pending;check:status IN ('pending','running','completed','failed','expired')"`
	StartedAt    *time.Time  `json:"started_at,omitempty" db:"started_at"`
	CompletedAt  *time.Time  `json:"completed_at,omitempty" db:"completed_at"`
	ExpiresAt    *time.Time  `json:"expires_at,omitempty" db:"expires_at"`
	WorkerNodeID *string     `json:"worker_node_id,omitempty" db:"worker_node_id" gorm:"type:varchar(100)"`
	SessionData  interface{} `json:"session_data,omitempty" db:"session_data" gorm:"type:jsonb"`
	ResourceUsage interface{} `json:"resource_usage,omitempty" db:"resource_usage" gorm:"type:jsonb"`
	CreatedAt    time.Time   `json:"created_at" db:"created_at" gorm:"autoCreateTime"`
	UpdatedAt    time.Time   `json:"updated_at" db:"updated_at" gorm:"autoUpdateTime"`

	// Relationships
	User     User     `json:"user,omitempty" gorm:"foreignKey:UserID"`
	Lab      Lab      `json:"lab,omitempty" gorm:"foreignKey:LabID"`
	LabSpec  *LabSpec `json:"lab_spec,omitempty" gorm:"foreignKey:LabSpecID;references:ID"`
}

// UserProgress represents user progress with composite primary key
type UserProgress struct {
	ID                 uuid.UUID   `json:"id" db:"id" gorm:"type:uuid;uniqueIndex;not null"`
	UserID             uuid.UUID   `json:"user_id" db:"user_id" gorm:"type:uuid;primaryKey"`
	LabID              uuid.UUID   `json:"lab_id" db:"lab_id" gorm:"type:uuid;primaryKey"`
	SessionID          *uuid.UUID  `json:"session_id,omitempty" db:"session_id" gorm:"type:uuid;index"`
	ProgressPercentage float64     `json:"progress_percentage" db:"progress_percentage" gorm:"type:decimal(5,2);default:0.00;check:progress_percentage >= 0 AND progress_percentage <= 100"`
	CompletedTasks     interface{} `json:"completed_tasks,omitempty" db:"completed_tasks" gorm:"type:jsonb"`
	CurrentTask        *string     `json:"current_task,omitempty" db:"current_task" gorm:"type:varchar(100)"`
	Score              *float64    `json:"score,omitempty" db:"score" gorm:"type:decimal(5,2)"`
	Attempts           int         `json:"attempts" db:"attempts" gorm:"default:1"`
	FirstAttemptAt     time.Time   `json:"first_attempt_at" db:"first_attempt_at" gorm:"autoCreateTime"`
	LastAttemptAt      time.Time   `json:"last_attempt_at" db:"last_attempt_at" gorm:"autoUpdateTime"`
	CompletedAt        *time.Time  `json:"completed_at,omitempty" db:"completed_at"`

	// Relationships
	User    User     `json:"user,omitempty" gorm:"foreignKey:UserID"`
	Lab     Lab      `json:"lab,omitempty" gorm:"foreignKey:LabID"`
	Session *Session `json:"session,omitempty" gorm:"foreignKey:SessionID"`
}

// UserRegistrationRequest represents user registration payload
type UserRegistrationRequest struct {
	Username  string  `json:"username" binding:"required,min=3,max=50"`
	Email     string  `json:"email" binding:"required,email"`
	Password  string  `json:"password" binding:"required,min=8"`
	FirstName *string `json:"first_name,omitempty"`
	LastName  *string `json:"last_name,omitempty"`
}

// UserLoginRequest represents user login payload
type UserLoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// UserResponse represents user data in responses (without password)
type UserResponse struct {
	ID          uuid.UUID  `json:"id"`
	Username    string     `json:"username"`
	Email       string     `json:"email"`
	FirstName   *string    `json:"first_name,omitempty"`
	LastName    *string    `json:"last_name,omitempty"`
	Role        string     `json:"role"`
	IsActive    bool       `json:"is_active"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastLoginAt *time.Time `json:"last_login_at,omitempty"`
}

// Sidecar Architecture Models for Lab Specifications

// ContainerSpec represents a container specification in the sidecar architecture
type ContainerSpec struct {
	Name            string                 `json:"name" binding:"required"`
	Image           string                 `json:"image" binding:"required"`
	Ports           []ContainerPort        `json:"ports,omitempty"`
	Env             []EnvVar               `json:"env,omitempty"`
	Resources       ResourceRequirements   `json:"resources,omitempty"`
	VolumeMounts    []VolumeMount          `json:"volume_mounts,omitempty"`
	SecurityContext *SecurityContext       `json:"security_context,omitempty"`
	Command         []string               `json:"command,omitempty"`
	Args            []string               `json:"args,omitempty"`
	Config          map[string]interface{} `json:"config,omitempty"`
}

// ContainerPort represents a port exposed by a container
type ContainerPort struct {
	Name          string `json:"name"`
	ContainerPort int32  `json:"container_port" binding:"required,min=1,max=65535"`
	Protocol      string `json:"protocol,omitempty"` // TCP, UDP
}

// EnvVar represents an environment variable
type EnvVar struct {
	Name      string    `json:"name" binding:"required"`
	Value     string    `json:"value,omitempty"`
	ValueFrom *EnvVarSource `json:"value_from,omitempty"`
}

// EnvVarSource represents the source of an environment variable value
type EnvVarSource struct {
	FieldRef    *ObjectFieldSelector `json:"field_ref,omitempty"`
	SecretKeyRef *SecretKeySelector   `json:"secret_key_ref,omitempty"`
}

// ObjectFieldSelector selects an API version field of an object
type ObjectFieldSelector struct {
	APIVersion string `json:"api_version,omitempty"`
	FieldPath  string `json:"field_path" binding:"required"`
}

// SecretKeySelector selects a key of a Secret
type SecretKeySelector struct {
	Name string `json:"name" binding:"required"`
	Key  string `json:"key" binding:"required"`
}

// ResourceRequirements describes resource requirements for a container
type ResourceRequirements struct {
	Limits   ResourceList `json:"limits,omitempty"`
	Requests ResourceList `json:"requests,omitempty"`
}

// ResourceList represents a list of compute resources
type ResourceList struct {
	CPU     string `json:"cpu,omitempty"`    // e.g., "100m", "0.5", "1"
	Memory  string `json:"memory,omitempty"` // e.g., "128Mi", "512Mi", "1Gi"
	Storage string `json:"storage,omitempty"` // e.g., "1Gi", "5Gi"
}

// VolumeMount describes a mounting of a Volume within a container
type VolumeMount struct {
	Name        string `json:"name" binding:"required"`
	MountPath   string `json:"mount_path" binding:"required"`
	SubPath     string `json:"sub_path,omitempty"`
	ReadOnly    bool   `json:"read_only,omitempty"`
}

// SecurityContext holds security configuration for a container
type SecurityContext struct {
	RunAsUser                *int64       `json:"run_as_user,omitempty"`
	RunAsGroup               *int64       `json:"run_as_group,omitempty"`
	RunAsNonRoot             *bool        `json:"run_as_non_root,omitempty"`
	ReadOnlyRootFilesystem   *bool        `json:"read_only_root_filesystem,omitempty"`
	AllowPrivilegeEscalation *bool        `json:"allow_privilege_escalation,omitempty"`
	Privileged               *bool        `json:"privileged,omitempty"`
	Capabilities             *Capabilities `json:"capabilities,omitempty"`
}

// Capabilities adds and removes POSIX capabilities from running containers
type Capabilities struct {
	Add  []string `json:"add,omitempty"`
	Drop []string `json:"drop,omitempty"`
}

// VolumeSpec represents a volume specification
type VolumeSpec struct {
	Name      string                 `json:"name" binding:"required"`
	Type      string                 `json:"type" binding:"required"` // emptyDir, configMap, secret, hostPath
	Config    map[string]interface{} `json:"config,omitempty"`
}

// NetworkSpec represents network configuration for the lab
type NetworkSpec struct {
	PodIP    string            `json:"pod_ip,omitempty"`
	VMIP     string            `json:"vm_ip,omitempty"`
	VMIPMode string            `json:"vm_ip_mode,omitempty"` // calculated, static, dhcp
	Ports    map[string]int32  `json:"ports,omitempty"`      // service_name -> port
}

// SidecarLabSpec represents the complete sidecar lab specification
type SidecarLabSpec struct {
	MainContainer    ContainerSpec   `json:"main_container" binding:"required"`
	TerminalSidecar  ContainerSpec   `json:"terminal_sidecar" binding:"required"`
	VSCodeSidecar    *ContainerSpec  `json:"vscode_sidecar,omitempty"`
	InitContainers   []ContainerSpec `json:"init_containers,omitempty"`
	SharedVolumes    []VolumeSpec    `json:"shared_volumes" binding:"required"`
	NetworkConfig    NetworkSpec     `json:"network_config"`
	ServicePorts     []ServicePort   `json:"service_ports,omitempty"`
	TimeoutSec       int32           `json:"timeout_sec" binding:"min=60,max=7200"`
	RestartPolicy    string          `json:"restart_policy,omitempty"` // Never, OnFailure, Always
}

// ServicePort represents a port exposed by the Kubernetes service
type ServicePort struct {
	Name       string `json:"name" binding:"required"`
	Port       int32  `json:"port" binding:"required,min=1,max=65535"`
	TargetPort int32  `json:"target_port" binding:"required,min=1,max=65535"`
	Protocol   string `json:"protocol,omitempty"` // TCP, UDP
}

// SessionEndpoints represents the endpoints available for a lab session
type SessionEndpoints struct {
	SessionID      string            `json:"session_id"`
	TerminalURL    string            `json:"terminal_url"`
	VSCodeURL      string            `json:"vscode_url,omitempty"`
	SSHURL         string            `json:"ssh_url,omitempty"`
	CustomPorts    map[string]string `json:"custom_ports,omitempty"`
	InternalIPs    NetworkInfo       `json:"internal_ips"`
}

// NetworkInfo provides internal network information
type NetworkInfo struct {
	PodIP string `json:"pod_ip"`
	VMIP  string `json:"vm_ip,omitempty"`
}