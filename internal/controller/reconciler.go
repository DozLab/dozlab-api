package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	// RequeueDelay is the standard delay for requeue
	RequeueDelay = 30 * time.Second

	// RequeueDelayError is the delay when an error occurs
	RequeueDelayError = 10 * time.Second

	// MaxConcurrentReconciles is the maximum number of concurrent reconciliations
	MaxConcurrentReconciles = 10
)

// LabSessionReconciler reconciles a LabSession object
type LabSessionReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
}

// +kubebuilder:rbac:groups=dozlab.io,resources=labsessions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=dozlab.io,resources=labsessions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=dozlab.io,resources=labsessions/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile is the main reconciliation loop
func (r *LabSessionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues("labsession", req.NamespacedName)
	logger.Info("Reconciling LabSession")

	// Fetch the LabSession instance
	session := &LabSession{}
	if err := r.Get(ctx, req.NamespacedName, session); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("LabSession resource not found, ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get LabSession")
		return ctrl.Result{RequeueAfter: RequeueDelayError}, err
	}

	// Handle deletion
	if !session.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, session)
	}

	// Add finalizer if not present
	if !controllerutil.ContainsFinalizer(session, FinalizerName) {
		controllerutil.AddFinalizer(session, FinalizerName)
		if err := r.Update(ctx, session); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{RequeueAfter: RequeueDelayError}, err
		}
		logger.Info("Finalizer added")
		return ctrl.Result{Requeue: true}, nil
	}

	// Reconcile based on current phase
	switch session.Status.Phase {
	case "":
		// New session, initialize
		return r.handleInitialize(ctx, session)
	case SessionPhasePending, SessionPhaseCreating:
		// Creating resources
		return r.handleCreating(ctx, session)
	case SessionPhaseRunning:
		// Monitor running session
		return r.handleRunning(ctx, session)
	case SessionPhaseFailed:
		// Handle failed session
		return r.handleFailed(ctx, session)
	default:
		// Unknown phase, reset to pending
		return r.updatePhase(ctx, session, SessionPhasePending, "Resetting unknown phase")
	}
}

// handleInitialize initializes a new lab session
func (r *LabSessionReconciler) handleInitialize(ctx context.Context, session *LabSession) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Initializing lab session")

	// Set default configuration
	r.setDefaults(session)

	// Validate the session spec
	if err := r.validateSpec(session); err != nil {
		r.Recorder.Event(session, corev1.EventTypeWarning, "ValidationFailed", err.Error())
		return r.updatePhase(ctx, session, SessionPhaseFailed, fmt.Sprintf("Validation failed: %v", err))
	}

	// Update to Pending phase
	return r.updatePhase(ctx, session, SessionPhasePending, "Session initialized successfully")
}

// handleCreating handles the creation of resources
func (r *LabSessionReconciler) handleCreating(ctx context.Context, session *LabSession) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Creating lab session resources")

	builder := NewResourceBuilder(session)

	// Create PVCs first
	pvcs := builder.BuildPVCs()
	for _, pvc := range pvcs {
		if err := r.createOrUpdateResource(ctx, session, pvc); err != nil {
			r.Recorder.Event(session, corev1.EventTypeWarning, "PVCCreationFailed", err.Error())
			return r.updatePhase(ctx, session, SessionPhaseFailed, fmt.Sprintf("PVC creation failed: %v", err))
		}
	}

	// Wait for PVCs to be bound
	for _, pvc := range pvcs {
		existingPVC := &corev1.PersistentVolumeClaim{}
		if err := r.Get(ctx, types.NamespacedName{Name: pvc.Name, Namespace: pvc.Namespace}, existingPVC); err != nil {
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		if existingPVC.Status.Phase != corev1.ClaimBound {
			logger.Info("Waiting for PVC to be bound", "pvc", pvc.Name)
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	// Create Secret
	secret := builder.BuildSecret()
	if err := r.createOrUpdateResource(ctx, session, secret); err != nil {
		r.Recorder.Event(session, corev1.EventTypeWarning, "SecretCreationFailed", err.Error())
		return r.updatePhase(ctx, session, SessionPhaseFailed, fmt.Sprintf("Secret creation failed: %v", err))
	}

	// Create Pod
	pod := builder.BuildPod()
	if err := r.createOrUpdateResource(ctx, session, pod); err != nil {
		r.Recorder.Event(session, corev1.EventTypeWarning, "PodCreationFailed", err.Error())
		return r.updatePhase(ctx, session, SessionPhaseFailed, fmt.Sprintf("Pod creation failed: %v", err))
	}

	// Create Service
	service := builder.BuildService()
	if err := r.createOrUpdateResource(ctx, session, service); err != nil {
		r.Recorder.Event(session, corev1.EventTypeWarning, "ServiceCreationFailed", err.Error())
		return r.updatePhase(ctx, session, SessionPhaseFailed, fmt.Sprintf("Service creation failed: %v", err))
	}

	// Update status with resource names
	session.Status.PodName = pod.Name
	session.Status.ServiceName = service.Name
	session.Status.ObservedGeneration = session.Generation

	// Move to Creating phase
	r.Recorder.Event(session, corev1.EventTypeNormal, "ResourcesCreated", "Lab session resources created successfully")
	return r.updatePhase(ctx, session, SessionPhaseCreating, "Resources created, waiting for pod to be ready")
}

// handleRunning monitors a running lab session
func (r *LabSessionReconciler) handleRunning(ctx context.Context, session *LabSession) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Monitoring running lab session")

	// Check pod status
	pod := &corev1.Pod{}
	podName := types.NamespacedName{
		Name:      session.Status.PodName,
		Namespace: session.Namespace,
	}

	if err := r.Get(ctx, podName, pod); err != nil {
		if errors.IsNotFound(err) {
			logger.Info("Pod not found, moving to failed state")
			return r.updatePhase(ctx, session, SessionPhaseFailed, "Pod disappeared unexpectedly")
		}
		return ctrl.Result{RequeueAfter: RequeueDelayError}, err
	}

	// Check if pod is still running
	switch pod.Status.Phase {
	case corev1.PodSucceeded:
		logger.Info("Pod completed successfully")
		return r.updatePhase(ctx, session, SessionPhaseTerminated, "Session completed successfully")
	case corev1.PodFailed:
		logger.Info("Pod failed")
		return r.updatePhase(ctx, session, SessionPhaseFailed, fmt.Sprintf("Pod failed: %s", pod.Status.Reason))
	case corev1.PodRunning:
		// Update status with pod IP and endpoints
		if session.Status.PodIP != pod.Status.PodIP {
			session.Status.PodIP = pod.Status.PodIP
			session.Status.VMIP = calculateVMIP(pod.Status.PodIP)
			session.Status.Endpoints = r.buildEndpoints(session, pod)
			if err := r.Status().Update(ctx, session); err != nil {
				return ctrl.Result{RequeueAfter: RequeueDelayError}, err
			}
		}

		// Check if we need to handle timeout
		if r.shouldTimeout(session) {
			logger.Info("Session timeout reached")
			return r.handleTimeout(ctx, session)
		}

		// Requeue to check again later
		return ctrl.Result{RequeueAfter: 1 * time.Minute}, nil
	default:
		// Still starting up
		logger.Info("Pod still starting", "phase", pod.Status.Phase)
		// Check if all containers are ready
		if r.isPodReady(pod) {
			logger.Info("Pod is ready, transitioning to Running")
			session.Status.PodIP = pod.Status.PodIP
			session.Status.VMIP = calculateVMIP(pod.Status.PodIP)
			session.Status.Endpoints = r.buildEndpoints(session, pod)
			session.Status.StartTime = &metav1.Time{Time: time.Now()}
			r.Recorder.Event(session, corev1.EventTypeNormal, "SessionReady", "Lab session is ready")
			return r.updatePhase(ctx, session, SessionPhaseRunning, "Session is running")
		}
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
}

// handleFailed handles a failed lab session
func (r *LabSessionReconciler) handleFailed(ctx context.Context, session *LabSession) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Lab session is in failed state")

	// Don't requeue failed sessions automatically
	// User must delete and recreate
	return ctrl.Result{}, nil
}

// handleDeletion handles the deletion of a lab session
func (r *LabSessionReconciler) handleDeletion(ctx context.Context, session *LabSession) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling lab session deletion")

	if controllerutil.ContainsFinalizer(session, FinalizerName) {
		// Update phase to Terminating
		if session.Status.Phase != SessionPhaseTerminating {
			session.Status.Phase = SessionPhaseTerminating
			session.Status.Message = "Cleaning up resources"
			session.Status.LastTransitionTime = &metav1.Time{Time: time.Now()}
			if err := r.Status().Update(ctx, session); err != nil {
				return ctrl.Result{RequeueAfter: RequeueDelayError}, err
			}
		}

		// Perform cleanup - resources with OwnerReferences will be automatically deleted
		// but we can add explicit cleanup here if needed

		logger.Info("Removing finalizer")
		controllerutil.RemoveFinalizer(session, FinalizerName)
		if err := r.Update(ctx, session); err != nil {
			return ctrl.Result{RequeueAfter: RequeueDelayError}, err
		}

		r.Recorder.Event(session, corev1.EventTypeNormal, "Deleted", "Lab session deleted successfully")
	}

	return ctrl.Result{}, nil
}

// handleTimeout handles session timeout
func (r *LabSessionReconciler) handleTimeout(ctx context.Context, session *LabSession) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Handling session timeout")

	// Delete the session
	if err := r.Delete(ctx, session); err != nil {
		return ctrl.Result{RequeueAfter: RequeueDelayError}, err
	}

	r.Recorder.Event(session, corev1.EventTypeNormal, "Timeout", "Session timeout reached, cleaning up")
	return ctrl.Result{}, nil
}

// Helper functions

func (r *LabSessionReconciler) setDefaults(session *LabSession) {
	if session.Spec.Resources.Memory == "" {
		session.Spec.Resources.Memory = "4Gi"
	}
	if session.Spec.Resources.CPU == "" {
		session.Spec.Resources.CPU = "2"
	}
	if session.Spec.Resources.Storage == "" {
		session.Spec.Resources.Storage = "10Gi"
	}
	if session.Spec.Timeout == "" {
		session.Spec.Timeout = "2h"
	}
	if session.Spec.Config.VSCodePassword == "" {
		session.Spec.Config.VSCodePassword = fmt.Sprintf("lab-%s", session.Spec.SessionID[:8])
	}
	// Enable all services by default
	session.Spec.Config.EnableTerminal = true
	session.Spec.Config.EnableVSCode = true
	session.Spec.Config.EnableSSH = true
}

func (r *LabSessionReconciler) validateSpec(session *LabSession) error {
	if session.Spec.UserID == "" {
		return fmt.Errorf("userId is required")
	}
	if session.Spec.SessionID == "" {
		return fmt.Errorf("sessionId is required")
	}
	return nil
}

func (r *LabSessionReconciler) createOrUpdateResource(ctx context.Context, session *LabSession, obj client.Object) error {
	logger := log.FromContext(ctx)

	// Set owner reference
	if err := controllerutil.SetControllerReference(session, obj, r.Scheme); err != nil {
		return fmt.Errorf("failed to set owner reference: %w", err)
	}

	// Try to create
	if err := r.Create(ctx, obj); err != nil {
		if errors.IsAlreadyExists(err) {
			logger.Info("Resource already exists", "resource", obj.GetName())
			return nil
		}
		return fmt.Errorf("failed to create resource: %w", err)
	}

	logger.Info("Resource created", "resource", obj.GetName())
	return nil
}

func (r *LabSessionReconciler) updatePhase(ctx context.Context, session *LabSession, phase SessionPhase, message string) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	if session.Status.Phase != phase {
		session.Status.Phase = phase
		session.Status.Message = message
		session.Status.LastTransitionTime = &metav1.Time{Time: time.Now()}
		session.Status.ObservedGeneration = session.Generation

		if err := r.Status().Update(ctx, session); err != nil {
			logger.Error(err, "Failed to update status")
			return ctrl.Result{RequeueAfter: RequeueDelayError}, err
		}

		logger.Info("Phase updated", "phase", phase, "message", message)
	}

	// Requeue based on phase
	if phase == SessionPhasePending || phase == SessionPhaseCreating {
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	return ctrl.Result{RequeueAfter: RequeueDelay}, nil
}

func (r *LabSessionReconciler) isPodReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}

	for _, cond := range pod.Status.Conditions {
		if cond.Type == corev1.PodReady && cond.Status == corev1.ConditionTrue {
			return true
		}
	}

	return false
}

func (r *LabSessionReconciler) shouldTimeout(session *LabSession) bool {
	if session.Status.StartTime == nil {
		return false
	}

	timeout, err := time.ParseDuration(session.Spec.Timeout)
	if err != nil {
		// Default to 2 hours
		timeout = 2 * time.Hour
	}

	return time.Since(session.Status.StartTime.Time) > timeout
}

func (r *LabSessionReconciler) buildEndpoints(session *LabSession, pod *corev1.Pod) map[string]string {
	endpoints := make(map[string]string)
	serviceName := fmt.Sprintf("lab-service-%s", session.Spec.SessionID)

	endpoints["terminal"] = fmt.Sprintf("ws://%s.%s.svc.cluster.local:8081", serviceName, session.Namespace)
	endpoints["ssh"] = fmt.Sprintf("%s.%s.svc.cluster.local:22", serviceName, session.Namespace)

	if session.Spec.Config.EnableVSCode {
		endpoints["vscode"] = fmt.Sprintf("http://%s.%s.svc.cluster.local:8080", serviceName, session.Namespace)
	}

	return endpoints
}

func calculateVMIP(podIP string) string {
	if podIP == "" {
		return ""
	}

	// Simple IP calculation: increment last octet by 1
	// In production, you'd want more robust IP management
	parts := []byte(podIP)
	if len(parts) > 0 {
		parts[len(parts)-1]++
	}
	return string(parts)
}

// SetupWithManager sets up the controller with the Manager
func (r *LabSessionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&LabSession{}).
		Owns(&corev1.Pod{}).
		Owns(&corev1.Service{}).
		Owns(&corev1.Secret{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		WithOptions(controller.Options{
			MaxConcurrentReconciles: MaxConcurrentReconciles,
			RateLimiter: workqueue.NewItemExponentialFailureRateLimiter(
				RequeueDelayError,
				1*time.Minute,
			),
		}).
		WithEventFilter(predicate.Or(
			predicate.GenerationChangedPredicate{},
			predicate.LabelChangedPredicate{},
		)).
		Complete(r)
}
