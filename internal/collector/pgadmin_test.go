// Copyright 2024 - 2025 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

package collector

import (
	"context"
	"testing"

	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"

	"github.com/crunchydata/postgres-operator/internal/feature"
	"github.com/crunchydata/postgres-operator/internal/initialize"
	"github.com/crunchydata/postgres-operator/internal/naming"
	"github.com/crunchydata/postgres-operator/internal/testing/cmp"
	"github.com/crunchydata/postgres-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

func TestEnablePgAdminLogging(t *testing.T) {
	t.Run("NilInstrumentationSpec", func(t *testing.T) {
		gate := feature.NewGate()
		assert.NilError(t, gate.SetFromMap(map[string]bool{
			feature.OpenTelemetryLogs: true,
		}))

		ctx := feature.NewContext(context.Background(), gate)

		pgadmin := new(v1beta1.PGAdmin)
		configmap := &corev1.ConfigMap{ObjectMeta: naming.StandalonePGAdmin(pgadmin)}
		initialize.Map(&configmap.Data)
		err := EnablePgAdminLogging(ctx, pgadmin.Spec.Instrumentation, configmap)
		assert.NilError(t, err)

		assert.Assert(t, cmp.MarshalMatches(configmap.Data, `
collector.yaml: |
  # Generated by postgres-operator. DO NOT EDIT.
  # Your changes will not be saved.
  exporters:
    debug:
      verbosity: detailed
  extensions:
    file_storage/gunicorn:
      create_directory: true
      directory: /var/log/gunicorn/receiver
      fsync: true
    file_storage/pgadmin:
      create_directory: true
      directory: /var/log/pgadmin/receiver
      fsync: true
  processors:
    batch/1s:
      timeout: 1s
    batch/200ms:
      timeout: 200ms
    groupbyattrs/compact: {}
    resource/pgadmin:
      attributes:
      - action: insert
        key: k8s.container.name
        value: pgadmin
      - action: insert
        key: k8s.namespace.name
        value: ${env:K8S_POD_NAMESPACE}
      - action: insert
        key: k8s.pod.name
        value: ${env:K8S_POD_NAME}
    transform/pgadmin_log:
      log_statements:
      - context: log
        statements:
        - set(cache, ParseJSON(body))
        - merge_maps(attributes, ExtractPatterns(cache["message"], "(?P<webrequest>[A-Z]{3}.*?[\\d]{3})"),
          "insert")
        - set(severity_text, cache["level"])
        - set(time_unix_nano, Int(cache["time"]*1000000000))
        - set(severity_number, SEVERITY_NUMBER_DEBUG)  where severity_text == "DEBUG"
        - set(severity_number, SEVERITY_NUMBER_INFO)   where severity_text == "INFO"
        - set(severity_number, SEVERITY_NUMBER_WARN)   where severity_text == "WARNING"
        - set(severity_number, SEVERITY_NUMBER_ERROR)  where severity_text == "ERROR"
        - set(severity_number, SEVERITY_NUMBER_FATAL)  where severity_text == "CRITICAL"
  receivers:
    filelog/gunicorn:
      include:
      - /var/lib/pgadmin/logs/gunicorn.log
      storage: file_storage/gunicorn
    filelog/pgadmin:
      include:
      - /var/lib/pgadmin/logs/pgadmin.log
      storage: file_storage/pgadmin
  service:
    extensions:
    - file_storage/gunicorn
    - file_storage/pgadmin
    pipelines:
      logs/gunicorn:
        exporters:
        - debug
        processors:
        - resource/pgadmin
        - transform/pgadmin_log
        - batch/200ms
        - groupbyattrs/compact
        receivers:
        - filelog/gunicorn
      logs/pgadmin:
        exporters:
        - debug
        processors:
        - resource/pgadmin
        - transform/pgadmin_log
        - batch/200ms
        - groupbyattrs/compact
        receivers:
        - filelog/pgadmin
`))
	})

	t.Run("InstrumentationSpecDefined", func(t *testing.T) {
		gate := feature.NewGate()
		assert.NilError(t, gate.SetFromMap(map[string]bool{
			feature.OpenTelemetryLogs: true,
		}))

		ctx := feature.NewContext(context.Background(), gate)

		pgadmin := new(v1beta1.PGAdmin)
		pgadmin.Spec.Instrumentation = testInstrumentationSpec()

		configmap := &corev1.ConfigMap{ObjectMeta: naming.StandalonePGAdmin(pgadmin)}
		initialize.Map(&configmap.Data)
		err := EnablePgAdminLogging(ctx, pgadmin.Spec.Instrumentation, configmap)
		assert.NilError(t, err)

		assert.Assert(t, cmp.MarshalMatches(configmap.Data, `
collector.yaml: |
  # Generated by postgres-operator. DO NOT EDIT.
  # Your changes will not be saved.
  exporters:
    debug:
      verbosity: detailed
    googlecloud:
      log:
        default_log_name: opentelemetry.io/collector-exported-log
      project: google-project-name
  extensions:
    file_storage/gunicorn:
      create_directory: true
      directory: /var/log/gunicorn/receiver
      fsync: true
    file_storage/pgadmin:
      create_directory: true
      directory: /var/log/pgadmin/receiver
      fsync: true
  processors:
    batch/1s:
      timeout: 1s
    batch/200ms:
      timeout: 200ms
    groupbyattrs/compact: {}
    resource/pgadmin:
      attributes:
      - action: insert
        key: k8s.container.name
        value: pgadmin
      - action: insert
        key: k8s.namespace.name
        value: ${env:K8S_POD_NAMESPACE}
      - action: insert
        key: k8s.pod.name
        value: ${env:K8S_POD_NAME}
    transform/pgadmin_log:
      log_statements:
      - context: log
        statements:
        - set(cache, ParseJSON(body))
        - merge_maps(attributes, ExtractPatterns(cache["message"], "(?P<webrequest>[A-Z]{3}.*?[\\d]{3})"),
          "insert")
        - set(severity_text, cache["level"])
        - set(time_unix_nano, Int(cache["time"]*1000000000))
        - set(severity_number, SEVERITY_NUMBER_DEBUG)  where severity_text == "DEBUG"
        - set(severity_number, SEVERITY_NUMBER_INFO)   where severity_text == "INFO"
        - set(severity_number, SEVERITY_NUMBER_WARN)   where severity_text == "WARNING"
        - set(severity_number, SEVERITY_NUMBER_ERROR)  where severity_text == "ERROR"
        - set(severity_number, SEVERITY_NUMBER_FATAL)  where severity_text == "CRITICAL"
  receivers:
    filelog/gunicorn:
      include:
      - /var/lib/pgadmin/logs/gunicorn.log
      storage: file_storage/gunicorn
    filelog/pgadmin:
      include:
      - /var/lib/pgadmin/logs/pgadmin.log
      storage: file_storage/pgadmin
  service:
    extensions:
    - file_storage/gunicorn
    - file_storage/pgadmin
    pipelines:
      logs/gunicorn:
        exporters:
        - googlecloud
        processors:
        - resource/pgadmin
        - transform/pgadmin_log
        - batch/200ms
        - groupbyattrs/compact
        receivers:
        - filelog/gunicorn
      logs/pgadmin:
        exporters:
        - googlecloud
        processors:
        - resource/pgadmin
        - transform/pgadmin_log
        - batch/200ms
        - groupbyattrs/compact
        receivers:
        - filelog/pgadmin
`))
	})
}
