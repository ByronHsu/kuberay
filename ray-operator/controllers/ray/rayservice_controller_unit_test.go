package ray

import (
	"testing"

	"github.com/ray-project/kuberay/ray-operator/apis/ray/v1alpha1"
	"github.com/ray-project/kuberay/ray-operator/controllers/ray/common"
	"github.com/ray-project/kuberay/ray-operator/pkg/client/clientset/versioned/scheme"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	clientFake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGenerateRayClusterJsonHash(t *testing.T) {
	// `generateRayClusterJsonHash` will mute fields that will not trigger new RayCluster preparation. For example,
	// Autoscaler will update `Replicas` and `WorkersToDelete` when scaling up/down. Hence, `hash1` should be equal to
	// `hash2` in this case.
	cluster := v1alpha1.RayCluster{
		Spec: v1alpha1.RayClusterSpec{
			RayVersion: "2.4.0",
			WorkerGroupSpecs: []v1alpha1.WorkerGroupSpec{
				{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{},
					},
					Replicas:    pointer.Int32Ptr(2),
					MinReplicas: pointer.Int32Ptr(1),
					MaxReplicas: pointer.Int32Ptr(4),
				},
			},
		},
	}

	hash1, err := generateRayClusterJsonHash(cluster.Spec)
	assert.Nil(t, err)

	*cluster.Spec.WorkerGroupSpecs[0].Replicas++
	hash2, err := generateRayClusterJsonHash(cluster.Spec)
	assert.Nil(t, err)
	assert.Equal(t, hash1, hash2)

	// RayVersion will not be muted, so `hash3` should not be equal to `hash1`.
	cluster.Spec.RayVersion = "2.100.0"
	hash3, err := generateRayClusterJsonHash(cluster.Spec)
	assert.Nil(t, err)
	assert.NotEqual(t, hash1, hash3)
}

func TestCompareRayClusterJsonHash(t *testing.T) {
	cluster1 := v1alpha1.RayCluster{
		Spec: v1alpha1.RayClusterSpec{
			RayVersion: "2.4.0",
		},
	}
	cluster2 := cluster1.DeepCopy()
	cluster2.Spec.RayVersion = "2.100.0"
	equal, err := compareRayClusterJsonHash(cluster1.Spec, cluster2.Spec)
	assert.Nil(t, err)
	assert.False(t, equal)

	equal, err = compareRayClusterJsonHash(cluster1.Spec, cluster1.Spec)
	assert.Nil(t, err)
	assert.True(t, equal)
}

func TestInconsistentRayServiceStatuses(t *testing.T) {
	r := &RayServiceReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("RayService"),
	}

	timeNow := metav1.Now()
	oldStatus := v1alpha1.RayServiceStatuses{
		ActiveServiceStatus: v1alpha1.RayServiceStatus{
			RayClusterName: "new-cluster",
			DashboardStatus: v1alpha1.DashboardStatus{
				IsHealthy:            true,
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
			},
			ApplicationStatus: v1alpha1.AppStatus{
				Status:               "running",
				Message:              "OK",
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
			},
			ServeStatuses: []v1alpha1.ServeDeploymentStatus{
				{
					Name:                 "serve-1",
					Status:               "unhealthy",
					Message:              "error",
					LastUpdateTime:       &timeNow,
					HealthLastUpdateTime: &timeNow,
				},
			},
		},
		PendingServiceStatus: v1alpha1.RayServiceStatus{
			RayClusterName: "old-cluster",
			DashboardStatus: v1alpha1.DashboardStatus{
				IsHealthy:            true,
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
			},
			ApplicationStatus: v1alpha1.AppStatus{
				Status:               "stopped",
				Message:              "stopped",
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
			},
			ServeStatuses: []v1alpha1.ServeDeploymentStatus{
				{
					Name:                 "serve-1",
					Status:               "healthy",
					Message:              "Serve is healthy",
					LastUpdateTime:       &timeNow,
					HealthLastUpdateTime: &timeNow,
				},
			},
		},
		ServiceStatus: v1alpha1.WaitForDashboard,
	}

	// Test 1: Update ServiceStatus only.
	newStatus := oldStatus.DeepCopy()
	newStatus.ServiceStatus = v1alpha1.WaitForServeDeploymentReady
	assert.True(t, r.inconsistentRayServiceStatuses(oldStatus, *newStatus))

	// Test 2: Test RayServiceStatus
	newStatus = oldStatus.DeepCopy()
	newStatus.ActiveServiceStatus.DashboardStatus.LastUpdateTime = &metav1.Time{Time: timeNow.Add(1)}
	assert.False(t, r.inconsistentRayServiceStatuses(oldStatus, *newStatus))

	newStatus.ActiveServiceStatus.DashboardStatus.IsHealthy = !oldStatus.ActiveServiceStatus.DashboardStatus.IsHealthy
	assert.True(t, r.inconsistentRayServiceStatuses(oldStatus, *newStatus))
}

func TestInconsistentRayServiceStatus(t *testing.T) {
	timeNow := metav1.Now()
	oldStatus := v1alpha1.RayServiceStatus{
		RayClusterName: "cluster-1",
		DashboardStatus: v1alpha1.DashboardStatus{
			IsHealthy:            true,
			LastUpdateTime:       &timeNow,
			HealthLastUpdateTime: &timeNow,
		},
		ApplicationStatus: v1alpha1.AppStatus{
			Status:               "running",
			Message:              "Application is running",
			LastUpdateTime:       &timeNow,
			HealthLastUpdateTime: &timeNow,
		},
		ServeStatuses: []v1alpha1.ServeDeploymentStatus{
			{
				Name:                 "serve-1",
				Status:               "healthy",
				Message:              "Serve is healthy",
				LastUpdateTime:       &timeNow,
				HealthLastUpdateTime: &timeNow,
			},
		},
	}

	r := &RayServiceReconciler{
		Log: ctrl.Log.WithName("controllers").WithName("RayService"),
	}

	// Test 1: Only LastUpdateTime and HealthLastUpdateTime are updated.
	newStatus := oldStatus.DeepCopy()
	newStatus.DashboardStatus.LastUpdateTime = &metav1.Time{Time: timeNow.Add(1)}
	assert.False(t, r.inconsistentRayServiceStatus(oldStatus, *newStatus))

	// Test 2: Not only LastUpdateTime and HealthLastUpdateTime are updated.
	newStatus = oldStatus.DeepCopy()
	newStatus.DashboardStatus.LastUpdateTime = &metav1.Time{Time: timeNow.Add(1)}
	newStatus.DashboardStatus.IsHealthy = !oldStatus.DashboardStatus.IsHealthy
	assert.True(t, r.inconsistentRayServiceStatus(oldStatus, *newStatus))
}

func TestIsHeadPodRunningAndReady(t *testing.T) {
	// Create a new scheme with CRDs, Pod, Service schemes.
	newScheme := runtime.NewScheme()
	_ = v1alpha1.AddToScheme(newScheme)
	_ = corev1.AddToScheme(newScheme)

	// Mock data
	cluster := v1alpha1.RayCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-cluster",
			Namespace: "default",
		},
	}

	headPod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "head-pod",
			Namespace: cluster.ObjectMeta.Namespace,
			Labels: map[string]string{
				common.RayClusterLabelKey:  cluster.ObjectMeta.Name,
				common.RayNodeTypeLabelKey: string(v1alpha1.HeadNode),
			},
		},
		Status: corev1.PodStatus{
			Phase: corev1.PodPending,
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodScheduled,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}

	// Initialize a fake client with newScheme and runtimeObjects.
	runtimeObjects := []runtime.Object{}
	fakeClient := clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()

	// Initialize RayCluster reconciler.
	r := &RayServiceReconciler{
		Client:   fakeClient,
		Recorder: &record.FakeRecorder{},
		Scheme:   scheme.Scheme,
		Log:      ctrl.Log.WithName("controllers").WithName("RayService"),
	}

	// Test 1: There is no head pod. `isHeadPodRunningAndReady` should return false.
	// In addition, an error should be returned if the number of head pods is not 1.
	isReady, err := r.isHeadPodRunningAndReady(&cluster)
	assert.NotNil(t, err)
	assert.False(t, isReady)

	// Test 2: There is one head pod, but the pod is not running and ready.
	// `isHeadPodRunningAndReady` should return false, and no error should be returned.
	runtimeObjects = []runtime.Object{headPod}
	fakeClient = clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	r.Client = fakeClient
	isReady, err = r.isHeadPodRunningAndReady(&cluster)
	assert.Nil(t, err)
	assert.False(t, isReady)

	// Test 3: There is one head pod, and the pod is running and ready.
	// `isHeadPodRunningAndReady` should return true, and no error should be returned.
	runningHeadPod := headPod.DeepCopy()
	runningHeadPod.Status = corev1.PodStatus{
		Phase: corev1.PodRunning,
		Conditions: []corev1.PodCondition{
			{
				Type:   corev1.PodReady,
				Status: corev1.ConditionTrue,
			},
		},
	}
	runtimeObjects = []runtime.Object{runningHeadPod}
	fakeClient = clientFake.NewClientBuilder().WithScheme(newScheme).WithRuntimeObjects(runtimeObjects...).Build()
	r.Client = fakeClient
	isReady, err = r.isHeadPodRunningAndReady(&cluster)
	assert.Nil(t, err)
	assert.True(t, isReady)
}
