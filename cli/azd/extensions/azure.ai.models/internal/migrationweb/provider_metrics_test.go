// Copyright (c) Microsoft Corporation. All rights reserved.
// Licensed under the MIT License.

package migrationweb

import (
	"testing"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/monitor/armmonitor"
)

func TestMetricTotalsIncludesErrorStatusSeries(t *testing.T) {
	metric := &armmonitor.Metric{
		Timeseries: []*armmonitor.TimeSeriesElement{
			{
				Metadatavalues: []*armmonitor.MetadataValue{{
					Name:  &armmonitor.LocalizableString{Value: new("StatusCode")},
					Value: new("200"),
				}},
				Data: []*armmonitor.MetricValue{{Total: new(float64(18))}},
			},
			{
				Metadatavalues: []*armmonitor.MetadataValue{{
					Name:  &armmonitor.LocalizableString{Value: new("StatusCode")},
					Value: new("429"),
				}},
				Data: []*armmonitor.MetricValue{{Total: new(float64(2))}},
			},
		},
	}

	total, errors, hasStatus := metricTotals(metric)
	if total != 20 || errors != 2 || !hasStatus {
		t.Fatalf(
			"unexpected totals: total=%v errors=%v hasStatus=%v",
			total,
			errors,
			hasStatus,
		)
	}
}

func TestMetricTotalPointsCombinesSeriesAndNormalizesRate(t *testing.T) {
	timestamp := time.Date(2026, 9, 14, 8, 0, 0, 0, time.UTC)
	metric := &armmonitor.Metric{
		Timeseries: []*armmonitor.TimeSeriesElement{
			{Data: []*armmonitor.MetricValue{{
				TimeStamp: new(timestamp),
				Total:     new(float64(20)),
			}}},
			{Data: []*armmonitor.MetricValue{{
				TimeStamp: new(timestamp),
				Total:     new(float64(10)),
			}}},
		},
	}

	points := metricTotalPoints(metric, 15)
	if len(points) != 1 || points[0].Value != 2 {
		t.Fatalf("unexpected normalized points: %+v", points)
	}
}

func TestMetricsIntervalBoundsChartDensity(t *testing.T) {
	tests := []struct {
		duration time.Duration
		interval string
		minutes  float64
	}{
		{duration: time.Hour, interval: "PT5M", minutes: 5},
		{duration: 24 * time.Hour, interval: "PT15M", minutes: 15},
		{duration: 7 * 24 * time.Hour, interval: "PT1H", minutes: 60},
		{duration: 30 * 24 * time.Hour, interval: "PT6H", minutes: 360},
	}
	for _, test := range tests {
		interval, minutes := metricsInterval(test.duration)
		if interval != test.interval || minutes != test.minutes {
			t.Fatalf(
				"duration %v: got interval=%s minutes=%v",
				test.duration,
				interval,
				minutes,
			)
		}
	}
}

func TestMetricAverageUsesSampleCount(t *testing.T) {
	metric := &armmonitor.Metric{
		Timeseries: []*armmonitor.TimeSeriesElement{{
			Data: []*armmonitor.MetricValue{
				{Average: new(float64(100)), Count: new(float64(3))},
				{Average: new(float64(400)), Count: new(float64(1))},
			},
		}},
	}

	average, ok := metricAverage(metric)
	if !ok || average != 175 {
		t.Fatalf("unexpected average: value=%v ok=%v", average, ok)
	}
}
