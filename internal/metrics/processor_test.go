package metrics

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type (
	mockClient struct {
		batches                chan []APIMetric
		sendMetricsCalledCount int
		err                    error
	}

	mockTimeService struct {
		now        time.Time
		tickerChan chan time.Time
	}
)

func makeMockClient() mockClient {
	return mockClient{
		batches: make(chan []APIMetric, 10),
		err:     nil,
	}
}

func makeMockTimeService() mockTimeService {
	return mockTimeService{
		now:        time.Now(),
		tickerChan: make(chan time.Time),
	}
}

func (mc *mockClient) SendMetrics(mts []APIMetric) error {
	mc.sendMetricsCalledCount++
	mc.batches <- mts
	return mc.err
}

func (ts *mockTimeService) NewTicker(duration time.Duration) *time.Ticker {
	return &time.Ticker{
		C: ts.tickerChan,
	}
}

func (ts *mockTimeService) Now() time.Time {
	return ts.now
}

func TestProcessorBatches(t *testing.T) {
	mc := makeMockClient()
	mts := makeMockTimeService()

	mts.now, _ = time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
	nowUnix := float64(mts.now.Unix())

	processor := MakeProcessor(&mc, &mts, 1000, false)

	d1 := Distribution{
		Name:   "metric-1",
		Tags:   []string{"a", "b", "c"},
		Values: []float64{1, 2, 3},
	}
	d2 := Distribution{
		Name:   "metric-1",
		Tags:   []string{"a", "b", "c"},
		Values: []float64{4, 5, 6},
	}

	processor.AddMetric(&d1)
	processor.AddMetric(&d2)

	processor.StartProcessing()
	processor.FinishProcessing()

	firstBatch := <-mc.batches

	assert.Equal(t, []APIMetric{{
		Name:       "metric-1",
		Tags:       []string{"a", "b", "c"},
		MetricType: DistributionType,
		Points: [][]float64{
			[]float64{nowUnix, 1},
			[]float64{nowUnix, 2},
			[]float64{nowUnix, 3},
			[]float64{nowUnix, 4},
			[]float64{nowUnix, 5},
			[]float64{nowUnix, 6},
		},
	}}, firstBatch)
}

func TestProcessorBatchesPerTick(t *testing.T) {
	mc := makeMockClient()
	mts := makeMockTimeService()

	firstTime, _ := time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")
	firstTimeUnix := float64(firstTime.Unix())
	secondTime, _ := time.Parse(time.RFC3339, "2007-01-02T15:04:05Z")
	secondTimeUnix := float64(secondTime.Unix())
	mts.now = firstTime

	processor := MakeProcessor(&mc, &mts, 1000, false)

	d1 := Distribution{
		Name:   "metric-1",
		Tags:   []string{"a", "b", "c"},
		Values: []float64{1, 2},
	}
	d2 := Distribution{
		Name:   "metric-1",
		Tags:   []string{"a", "b", "c"},
		Values: []float64{3},
	}
	d3 := Distribution{
		Name:   "metric-1",
		Tags:   []string{"a", "b", "c"},
		Values: []float64{4, 5},
	}
	d4 := Distribution{
		Name:   "metric-1",
		Tags:   []string{"a", "b", "c"},
		Values: []float64{6},
	}

	processor.StartProcessing()

	processor.AddMetric(&d1)
	processor.AddMetric(&d2)

	// This wait is necessary to make sure both metrics have been added to the batch
	<-time.Tick(time.Millisecond * 10)
	// Sending time to the ticker channel will flush the batch.
	mts.tickerChan <- firstTime
	firstBatch := <-mc.batches
	mts.now = secondTime

	processor.AddMetric(&d3)
	processor.AddMetric(&d4)

	processor.FinishProcessing()
	secondBatch := <-mc.batches
	batches := [][]APIMetric{firstBatch, secondBatch}

	assert.Equal(t, [][]APIMetric{
		[]APIMetric{
			{
				Name:       "metric-1",
				Tags:       []string{"a", "b", "c"},
				MetricType: DistributionType,
				Points: [][]float64{
					[]float64{firstTimeUnix, 1},
					[]float64{firstTimeUnix, 2},
					[]float64{firstTimeUnix, 3},
				},
			}},
		[]APIMetric{
			{
				Name:       "metric-1",
				Tags:       []string{"a", "b", "c"},
				MetricType: DistributionType,
				Points: [][]float64{
					[]float64{secondTimeUnix, 4},
					[]float64{secondTimeUnix, 5},
					[]float64{secondTimeUnix, 6},
				},
			}},
	}, batches)
}

func TestProcessorPerformsRetry(t *testing.T) {
	mc := makeMockClient()
	mts := makeMockTimeService()

	mts.now, _ = time.Parse(time.RFC3339, "2006-01-02T15:04:05Z")

	shouldRetry := true
	processor := MakeProcessor(&mc, &mts, 1000, shouldRetry)

	d1 := Distribution{
		Name:   "metric-1",
		Tags:   []string{"a", "b", "c"},
		Values: []float64{1, 2, 3},
	}

	mc.err = errors.New("Some error")

	processor.AddMetric(&d1)

	processor.FinishProcessing()

	assert.Equal(t, 3, mc.sendMetricsCalledCount)
}
