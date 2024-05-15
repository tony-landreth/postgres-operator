// Copyright 2023 - 2024 Crunchy Data Solutions, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package autogrow

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/crunchydata/postgres-operator/internal/controller/runtime"
	"github.com/crunchydata/postgres-operator/internal/naming"
	"github.com/crunchydata/postgres-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
	"github.com/go-logr/logr"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// There are 6 df output columns for df --human-readable: "Filesystem Size Used Avail Use% Mounted on".
// 4 of those columns are relevant.
const dfUsePercentIdx int = 4

type Runner struct {
	refresh      time.Duration
	clusters     map[string][]string
	client       client.Client
	clientConfig *rest.Config
	log          logr.Logger
	podExec      func(
		namespace, pod, container string,
		stdin io.Reader, stdout, stderr io.Writer, command ...string,
	) error
}

// Runner implements [Autogrow] and [manager.Runnable].
var (
	_ Autogrow         = (*Runner)(nil)
	_ manager.Runnable = (*Runner)(nil)
)

func NewRunner(clientConfig *rest.Config, log logr.Logger) (*Runner, error) {
	runner := &Runner{
		refresh:      10 * time.Second, // TODO: Maybe time.Minute for production?
		clientConfig: clientConfig,
		log:          log,
	}

	return runner, nil
}

func (r *Runner) WatchCluster(clusterNamespace, clusterName string, client client.Client) {
	if r.clusters == nil {
		r.clusters = map[string][]string{}
	}
	if r.client == nil {
		r.client = client
	}
	key := clusterNamespace + "-" + clusterName
	if _, ok := r.clusters[key]; !ok {
		r.clusters[key] = []string{clusterNamespace, clusterName}
	}
}

func (r *Runner) getDiskUsage(keys []string, i int, err error) (string, error) {
	clusters := r.clusters
	k := keys[i]
	cluster := clusters[k]
	clusterNamespace := cluster[0]
	clusterName := cluster[1]
	pods := &corev1.PodList{}
	selector, _ := naming.AsSelector(naming.ClusterInstances(clusterName))
	ctx := context.Background()
	errors.WithStack(
		r.client.List(ctx, pods,
			client.InNamespace(clusterNamespace),
			client.MatchingLabelsSelector{Selector: selector},
		))
	if len(pods.Items) == 0 {
		// If no pods return, it may indicate that the cluster has been deleted.
		// Remove the key in a threadsafe way; the key will be replaced if a
		// startup delay is to blame.
		delete(clusters, k)
		return "", nil
	}
	// TODO: Properly request the primary.
	pod := pods.Items[0]
	podName := fmt.Sprintf(pod.ObjectMeta.Name)
	var stdin, stdout, stderr bytes.Buffer

	dfString := []string{"df", "--human-readable", "/pgdata"}
	r.podExec("postgres-operator", podName, "database", &stdout, &stdin, &stderr, dfString...)

	if stdin.String() != "" {
		dfValues := strings.Split(stdin.String(), "\n")[1]
		if percent := strings.Fields(dfValues)[dfUsePercentIdx]; strings.Contains(percent, "%") {
			return percent, nil
		}
	}
	return "", err
}

func (r *Runner) AnnotatePGDataPVC(clusterNamespace, clusterName string) error {
	r.log.Error(errors.New("CLUSTER"), clusterNamespace+":"+clusterName)
	volumes := &corev1.PersistentVolumeClaimList{}
	selector, err := naming.AsSelector(naming.Cluster(clusterName))
	if err == nil {
		err = errors.WithStack(
			r.client.List(context.TODO(), volumes,
				client.InNamespace(clusterNamespace),
				client.MatchingLabelsSelector{Selector: selector},
			))
	}

	pvc := volumes.Items[0]
	err = r.client.Patch(context.TODO(), &pvc, client.RawPatch(
		client.Merge.Type(), []byte(`{"metadata":{"annotations":{"disk-starvation": "detected"}}}`)))
	return nil
}

// TODO: Rename this
func (r *Runner) checkUsage() error {
	// If the Runner isn't configured, do nothing.
	if len(r.clusters) == 0 || r.client == nil {
		return nil
	}

	clusters := r.clusters
	var keys []string
	for k := range clusters {
		keys = append(keys, k)
	}
	var wg sync.WaitGroup
	sliceLength := len(keys)
	wg.Add(sliceLength)
	var err error
	for i := 0; i < sliceLength; i++ {
		go func(i int) error {
			defer wg.Done()
			usage, err := r.getDiskUsage(keys, i, err)
			key := keys[i]
			// TODO: Test that the key exists or you'll blow up when a cluster is deleted.
			// Or test that the cluster exists.
			clusters := r.clusters[key]
			clusterNamespace := clusters[0]
			clusterName := clusters[1]
			r.log.Info(fmt.Sprintf("%s disk usage %s", keys[i], usage))

			if ExceedsUsageLimit(usage) {
				r.log.Error(errors.New("EXCEEDS"), clusterNamespace+":"+clusterName)
				r.AnnotatePGDataPVC(clusterNamespace, clusterName)
			}
			if err != nil {
				return err
			}
			return nil
		}(i)
	}
	return err
}

// NeedLeaderElection returns true so that r runs only on the single
// [manager.Manager] that is elected leader in the Kubernetes namespace.
func (r *Runner) NeedLeaderElection() bool { return true }

func (r *Runner) Start(ctx context.Context) error {
	if r.podExec == nil {
		var err error
		r.podExec, err = runtime.NewPodExecutor(r.clientConfig)
		if err != nil {
			return err
		}
	}
	var ticks <-chan time.Time

	ticker := time.NewTicker(r.refresh)
	defer ticker.Stop()
	ticks = ticker.C

	for {
		select {
		case <-ticks:
			if err := r.checkUsage(); err != nil {
				r.log.Error(err, "Unable to retrieve disk utilization")
			}
		}
	}
}

type exec func(
	namespace, pod, container string,
	stdin io.Reader, stdout, stderr io.Writer, command ...string,
) error

func GetDiskUsage(cluster *v1beta1.PostgresCluster, cli client.Client, podExec exec) (string, error) {
	clusterNamespace := cluster.Namespace
	clusterName := cluster.Name
	pods := &corev1.PodList{}
	selector, _ := naming.AsSelector(naming.ClusterInstances(clusterName))
	ctx := context.Background()
	err := errors.WithStack(
		cli.List(ctx, pods,
			client.InNamespace(clusterNamespace),
			client.MatchingLabelsSelector{Selector: selector},
		))

	if len(pods.Items) == 0 {
		return "", errors.New("No pods found")
	}

	var primary v1.Pod
	for _, pod := range pods.Items {
		if pod.Labels[naming.LabelRole] == naming.RolePatroniLeader {
			primary = pod
		}
	}
	podName := fmt.Sprintf(primary.ObjectMeta.Name)
	var stdin, stdout, stderr bytes.Buffer
	dfString := []string{"df", "--human-readable", "/pgdata"}
	podExec("postgres-operator", podName, "database", &stdout, &stdin, &stderr, dfString...)

	if stdin.String() != "" {
		dfValues := strings.Split(stdin.String(), "\n")[1]
		if percent := strings.Fields(dfValues)[dfUsePercentIdx]; strings.Contains(percent, "%") {
			return percent, nil
		}
	}
	return "", err
}

func ExceedsUsageLimit(diskUse string) bool {
	percentString := strings.Split(diskUse, "%")[0]
	percentInt, _ := strconv.Atoi(percentString)
	return percentInt > 75
}

func DiskUseStatus(object client.Object, conditions *[]metav1.Condition, diskUse string) {
	if ExceedsUsageLimit(diskUse) {
		meta.SetStatusCondition(conditions, metav1.Condition{
			Type:               v1beta1.DiskStarved,
			Status:             metav1.ConditionTrue,
			Reason:             "DiskUsageAboveThreshold",
			Message:            fmt.Sprintf("Disk use is at %s", diskUse),
			ObservedGeneration: object.GetGeneration(),
		})
		return
	}
	meta.SetStatusCondition(conditions, metav1.Condition{
		Type:               v1beta1.DiskStarved,
		Status:             metav1.ConditionFalse,
		Reason:             "DiskUsageBelowThreshold",
		Message:            fmt.Sprintf("Disk use is at %s", diskUse),
		ObservedGeneration: object.GetGeneration(),
	})
}
