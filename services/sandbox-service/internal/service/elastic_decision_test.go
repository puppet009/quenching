package service

import (
	"github.com/quenching/sandbox-service/internal/model"
	"testing"
	"time"
)

func testPolicy() model.ElasticPolicy {
	return model.ElasticPolicy{Enabled: true, Min: model.ResourceAmounts{CPUMilli: 100, MemoryBytes: 1000}, Max: model.ResourceAmounts{CPUMilli: 200, MemoryBytes: 2000}, MaxReplicas: 2, Current: model.ElasticTarget{Replicas: 1, Requests: model.ResourceAmounts{CPUMilli: 100, MemoryBytes: 1000}}, Limits: model.ResourceAmounts{CPUMilli: 200, MemoryBytes: 2000}}
}
func TestElasticHysteresisBoundsAndCooldown(t *testing.T) {
	p := testPolicy()
	now := time.Now()
	high := model.ResourceAmounts{CPUMilli: 90, MemoryBytes: 800}
	if d := DecideElastic(&p, high, now); d != nil {
		t.Fatal("one sample scaled")
	}
	d := DecideElastic(&p, high, now.Add(30*time.Second))
	if d == nil || d.Requests.CPUMilli != 125 || d.Requests.MemoryBytes != 1250 {
		t.Fatalf("bad increase: %+v", d)
	}
	p.Current = *d
	p.LastChanged = now.Add(30 * time.Second)
	if DecideElastic(&p, high, now.Add(time.Minute)) != nil {
		t.Fatal("ignored cooldown")
	}
	p.Current.Requests = p.Max
	p.LastChanged = time.Time{}
	p.HighSamples = 1
	d = DecideElastic(&p, model.ResourceAmounts{CPUMilli: 190, MemoryBytes: 1200}, now.Add(3*time.Minute))
	if d == nil || d.Replicas != 2 || !d.Requests.Fits(p.Max) {
		t.Fatalf("bad horizontal increase: %+v", d)
	}
}
func TestElasticLowLoadRequiresFiveMinutesAndNeverBelowMinimum(t *testing.T) {
	p := testPolicy()
	p.Current.Requests = p.Max
	now := time.Now()
	if DecideElastic(&p, model.ResourceAmounts{}, now) != nil {
		t.Fatal("immediate decrease")
	}
	for i := 1; i < 10; i++ {
		if DecideElastic(&p, model.ResourceAmounts{}, now.Add(time.Duration(i)*30*time.Second)) != nil {
			t.Fatal("early decrease")
		}
	}
	d := DecideElastic(&p, model.ResourceAmounts{}, now.Add(5*time.Minute))
	if d == nil || d.Requests.CPUMilli != 160 {
		t.Fatalf("no decrease %+v", d)
	}
	p.Current.Requests = p.Min
	p.LowSince = now
	p.LastSample = time.Time{}
	if DecideElastic(&p, model.ResourceAmounts{}, now.Add(6*time.Minute)) != nil {
		t.Fatal("below minimum")
	}
}
func TestElasticMetricsMustBeCompleteAndFresh(t *testing.T) {
	now := time.Now()
	for _, m := range []map[string]string{nil, {"cpu": "0"}, {"cpu": "bad", "memory": "1Mi"}, {"cpu": "-1", "memory": "1Mi"}} {
		if _, ok := parseMetricAmount(m); ok {
			t.Fatalf("accepted invalid metric %v", m)
		}
	}
	if _, ok := FreshUsage(&model.ResourceUsage{Timestamp: now.Add(-3 * time.Minute).Format(time.RFC3339)}, now); ok {
		t.Fatal("stale accepted")
	}
	if _, ok := FreshUsage(&model.ResourceUsage{Timestamp: now.Add(time.Minute).Format(time.RFC3339)}, now); ok {
		t.Fatal("future accepted")
	}
	if v, ok := parseMetricAmount(map[string]string{"cpu": "50m", "memory": "1Mi"}); !ok || v.CPUMilli != 50 {
		t.Fatal("valid metric rejected")
	}
}
