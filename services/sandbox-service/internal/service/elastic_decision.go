package service

import (
	"github.com/quenching/sandbox-service/internal/model"
	"k8s.io/apimachinery/pkg/api/resource"
	"time"
)

func parseMetricAmount(m map[string]string) (model.ResourceAmounts, bool) {
	c, e := resource.ParseQuantity(m["cpu"])
	if e != nil || c.Sign() < 0 {
		return model.ResourceAmounts{}, false
	}
	b, e := resource.ParseQuantity(m["memory"])
	if e != nil || b.Sign() < 0 {
		return model.ResourceAmounts{}, false
	}
	return model.ResourceAmounts{CPUMilli: c.MilliValue(), MemoryBytes: b.Value()}, true
}
func FreshUsage(u *model.ResourceUsage, now time.Time) (model.ResourceAmounts, bool) {
	if u == nil {
		return model.ResourceAmounts{}, false
	}
	at, e := time.Parse(time.RFC3339Nano, u.Timestamp)
	if e != nil || at.After(now.Add(5*time.Second)) || now.Sub(at) > 2*time.Minute {
		return model.ResourceAmounts{}, false
	}
	return u.CPU, true
}
func ResetElasticSamples(p *model.ElasticPolicy) {
	p.HighSamples = 0
	p.LowSince = time.Time{}
	p.LastSample = time.Time{}
}

// usage is per-replica mean, with every ready replica represented by fresh metrics.
func DecideElastic(p *model.ElasticPolicy, usage model.ResourceAmounts, now time.Time) *model.ElasticTarget {
	if !p.Enabled || p.Pending != nil || now.Sub(p.LastChanged) < 2*time.Minute {
		ResetElasticSamples(p)
		return nil
	}
	if !p.LastSample.IsZero() && now.Sub(p.LastSample) > 75*time.Second {
		ResetElasticSamples(p)
	}
	p.LastSample = now
	target := p.Current
	req := target.Requests
	cpuHigh := usage.CPUMilli*100 > req.CPUMilli*70
	memHigh := usage.MemoryBytes*100 > req.MemoryBytes*75
	if cpuHigh || memHigh {
		p.LowSince = time.Time{}
		p.HighSamples++
		if p.HighSamples < 2 {
			return nil
		}
		if cpuHigh && req.CPUMilli < p.Max.CPUMilli {
			target.Requests.CPUMilli = min(p.Max.CPUMilli, (req.CPUMilli*5+3)/4)
		}
		if memHigh && req.MemoryBytes < p.Max.MemoryBytes {
			target.Requests.MemoryBytes = min(p.Max.MemoryBytes, (req.MemoryBytes*5+3)/4)
		}
		if target == p.Current && cpuHigh && target.Replicas < p.MaxReplicas {
			target.Replicas++
		}
	} else if usage.CPUMilli*100 < req.CPUMilli*30 && usage.MemoryBytes*100 < req.MemoryBytes*30 {
		p.HighSamples = 0
		if p.LowSince.IsZero() {
			p.LowSince = now
		}
		if now.Sub(p.LowSince) < 5*time.Minute {
			return nil
		}
		if target.Replicas > 1 {
			target.Replicas--
		} else {
			target.Requests.CPUMilli = max(p.Min.CPUMilli, req.CPUMilli*4/5)
			target.Requests.MemoryBytes = max(p.Min.MemoryBytes, req.MemoryBytes*4/5)
		}
	} else {
		ResetElasticSamples(p)
	}
	if target == p.Current {
		return nil
	}
	ResetElasticSamples(p)
	return &target
}
