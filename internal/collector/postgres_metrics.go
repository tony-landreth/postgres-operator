// Copyright 2024 - 2025 Crunchy Data Solutions, Inc.
//
// SPDX-License-Identifier: Apache-2.0

// TODO fold this back into postgres.go once the collector package stabilizes.
package collector

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/crunchydata/postgres-operator/internal/feature"
	"github.com/crunchydata/postgres-operator/internal/pgmonitor"
	"github.com/crunchydata/postgres-operator/pkg/apis/postgres-operator.crunchydata.com/v1beta1"
)

// https://pkg.go.dev/embed
//
//go:embed "generated/postgres_5s_metrics.json"
var fiveSecondMetrics json.RawMessage

//go:embed "generated/postgres_5m_metrics.json"
var fiveMinuteMetrics json.RawMessage

//go:embed "generated/pgbackrest_metrics.json"
var pgBackRestMetrics json.RawMessage

// Set sqlQueryUsername = ccp_monitoring
func EnablePostgresMetrics(ctx context.Context, inCluster *v1beta1.PostgresCluster, config *Config) {
	if feature.Enabled(ctx, feature.OpenTelemetryMetrics) {
		// Add Prometheus exporter
		config.Exporters[Prometheus] = map[string]any{
			"endpoint": "0.0.0.0:8889",
		}

		// TODO: Include pgBackRestMetrics with other fiveSecondMetrics.
		config.Receivers[FiveSecondSqlQuery] = map[string]any{
			"driver":              "postgres",
			"datasource":          fmt.Sprintf(`host=localhost dbname=postgres port=5432 user=%s password=${env:PGPASSWORD} sslmode=disable`, pgmonitor.MonitoringUser),
			"collection_interval": "10s",
			// Give Postgres time to get ready.
			"initial_delay": "5s",
			"queries":       slices.Clone(fiveSecondMetrics),
		}

		config.Receivers[FiveMinuteSqlQuery] = map[string]any{
			"driver":              "postgres",
			"datasource":          fmt.Sprintf(`host=localhost dbname=postgres port=5432 user=%s password=${env:PGPASSWORD} sslmode=disable`, pgmonitor.MonitoringUser),
			"collection_interval": "300s",
			// Give Postgres time to get ready.
			"initial_delay": "10s",
			"queries":       slices.Clone(fiveMinuteMetrics),
		}

		// Add Metrics Pipeline
		config.Pipelines[Metrics] = Pipeline{
			Receivers: []ComponentID{FiveSecondSqlQuery, FiveMinuteSqlQuery},
			Exporters: []ComponentID{Prometheus},
		}
	}
}
