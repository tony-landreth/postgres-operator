package diskstats

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/crunchydata/postgres-operator/internal/naming"
	"github.com/crunchydata/postgres-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const dfUsePercentIdx int = 4

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

func DiskUseStatus(object client.Object, conditions *[]metav1.Condition, diskUse string) {
	percentString := strings.Split(diskUse, "%")[0]
	if percentString == "" {
		return
	}
	percentInt, _ := strconv.Atoi(percentString)

	if percentInt <= 75 {
		meta.SetStatusCondition(conditions, metav1.Condition{
			Type:               v1beta1.DiskStarved,
			Status:             metav1.ConditionFalse,
			Reason:             "DiskBelowUsageThreshold",
			Message:            fmt.Sprintf("Disk use is at %s", diskUse),
			ObservedGeneration: object.GetGeneration(),
		})
	} else {
		meta.SetStatusCondition(conditions, metav1.Condition{
			Type:               v1beta1.DiskStarved,
			Status:             metav1.ConditionTrue,
			Reason:             "DiskAboveUsageThreshold",
			Message:            fmt.Sprintf("Disk use is at %s", diskUse),
			ObservedGeneration: object.GetGeneration(),
		})
	}
}
