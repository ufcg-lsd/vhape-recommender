package samplewarmer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/logic/estimators"
	"k8s.io/autoscaler/vertical-pod-autoscaler/pkg/recommender/model"
)

func TestReadSamplesGroupsByWorkloadContainerAndResource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "samples.csv")
	csv := `timestamp, container_name, deployment_namespace, deployment_key, resource_type, value
1787220000, app, production, api, cpu, 125
1787220001, app, production, api, memory, 1048576
1755684002, worker, production, api, cpu, 250
`
	require.NoError(t, os.WriteFile(path, []byte(csv), 0o600))

	samples, err := readSamples(path)
	require.NoError(t, err)

	workload := samples[WorkloadKey{Namespace: "production", Name: "api"}]
	assert.Equal(t, []estimators.TimedSample{{
		Timestamp: time.Unix(1787220000, 0).UTC(),
		Value:     model.ResourceAmount(125),
	}}, workload["app"][model.ResourceCPU])
	assert.Equal(t, []estimators.TimedSample{{
		Timestamp: time.Unix(1787220001, 0).UTC(),
		Value:     model.ResourceAmount(1048576),
	}}, workload["app"][model.ResourceMemory])
	assert.Equal(t, model.ResourceAmount(250), workload["worker"][model.ResourceCPU][0].Value)
}

func TestReadSamplesRejectsUnsupportedResource(t *testing.T) {
	path := filepath.Join(t.TempDir(), "samples.csv")
	csv := `timestamp,container_name,deployment_namespace,deployment_key,resource_type,value
1787220000,app,production,api,gpu,1
`
	require.NoError(t, os.WriteFile(path, []byte(csv), 0o600))

	_, err := readSamples(path)
	require.ErrorContains(t, err, `unsupported resource_type "gpu"`)
}

func TestTransposeSamplesAlignsLatestTimestampWithT0(t *testing.T) {
	firstTimestamp := time.Unix(100, 0).UTC()
	lastTimestamp := time.Unix(160, 0).UTC()
	t0 := time.Unix(1000, 0).UTC()
	samples := []estimators.TimedSample{
		{Timestamp: firstTimestamp, Value: 10},
		{Timestamp: lastTimestamp, Value: 20},
	}

	transposed := transposeSamples(samples, lastTimestamp, t0)

	assert.Equal(t, t0.Add(-time.Minute), transposed[0].Timestamp)
	assert.Equal(t, t0, transposed[1].Timestamp)
	assert.Equal(t, samples[0].Value, transposed[0].Value)
	assert.Equal(t, samples[1].Value, transposed[1].Value)
	assert.Equal(t, firstTimestamp, samples[0].Timestamp, "original samples must not be mutated")
}

func TestLatestTimestampUsesAllContainersAndResources(t *testing.T) {
	expected := time.Unix(300, 0).UTC()
	samples := map[string]map[model.ResourceName][]estimators.TimedSample{
		"app": {
			model.ResourceCPU:    {{Timestamp: time.Unix(100, 0)}},
			model.ResourceMemory: {{Timestamp: expected}},
		},
		"sidecar": {
			model.ResourceCPU: {{Timestamp: time.Unix(200, 0)}},
		},
	}

	latest, found := latestTimestamp(samples)

	assert.True(t, found)
	assert.Equal(t, expected, latest)
}
