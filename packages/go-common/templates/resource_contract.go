package templates

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type ResourceAmounts struct {
	CPU    string `yaml:"cpu" json:"cpu"`
	Memory string `yaml:"memory" json:"memory"`
}
type ComponentResources struct {
	Requests ResourceAmounts `yaml:"requests" json:"requests"`
	Limits   ResourceAmounts `yaml:"limits" json:"limits"`
}

func ParseCPU(v string) (int64, error) {
	factor := 1000.0
	if strings.HasSuffix(v, "m") {
		v = strings.TrimSuffix(v, "m")
		factor = 1
	}
	n, e := strconv.ParseFloat(v, 64)
	n *= factor
	if e != nil || math.IsNaN(n) || math.IsInf(n, 0) || n < 1 || n > 1e9 || math.Abs(n-math.Round(n)) > 1e-6 {
		return 0, fmt.Errorf("invalid positive CPU quantity")
	}
	return int64(math.Round(n)), nil
}

var memoryQuantity = regexp.MustCompile(`^([0-9]+(?:\.[0-9]+)?)(Ki|Mi|Gi|Ti|K|M|G|T)?$`)

func ParseBytes(v string) (int64, error) {
	m := memoryQuantity.FindStringSubmatch(v)
	if m == nil {
		return 0, fmt.Errorf("invalid byte quantity %q", v)
	}
	n, e := strconv.ParseFloat(m[1], 64)
	units := map[string]float64{"": 1, "Ki": 1024, "Mi": 1048576, "Gi": 1073741824, "Ti": 1099511627776, "K": 1000, "M": 1e6, "G": 1e9, "T": 1e12}
	n *= units[m[2]]
	if e != nil || n < 1 || n > 9e15 {
		return 0, fmt.Errorf("invalid positive byte quantity")
	}
	return int64(math.Ceil(n)), nil
}
func (a ResourceAmounts) Values() (int64, int64, error) {
	c, e := ParseCPU(a.CPU)
	if e != nil {
		return 0, 0, e
	}
	m, e := ParseBytes(a.Memory)
	return c, m, e
}
func ValidateResourceContract(m Manifest) error {
	if m.Resources == nil {
		return nil
	}
	r := m.Resources
	lc, lm, e := (ResourceAmounts{r.CPU, r.Memory}).Values()
	if e != nil {
		return e
	}
	req := ResourceAmounts{r.CPU, r.Memory}
	if r.Requests != nil {
		req = *r.Requests
	}
	rc, rm, e := req.Values()
	if e != nil || rc > lc || rm > lm {
		return fmt.Errorf("template requests exceed limits or invalid")
	}
	if r.Storage != "" {
		if _, e = ParseBytes(r.Storage); e != nil {
			return e
		}
	}
	var sc, sm, slc, slm int64
	count := 0
	for _, s := range m.Services {
		if s.Elastic != nil && (s.Elastic.MaxReplicas < 1 || s.Elastic.MaxReplicas > 2 || s.Role == RoleMiddleware || s.Resources == nil) {
			return fmt.Errorf("service %s: elastic requires business resources and max_replicas 1-2", s.Name)
		}
		if s.Resources == nil {
			continue
		}
		count++
		c, b, e := s.Resources.Requests.Values()
		if e != nil {
			return e
		}
		x, y, e := s.Resources.Limits.Values()
		if e != nil || c > x || b > y {
			return fmt.Errorf("service %s has invalid resource limits", s.Name)
		}
		sc += c
		sm += b
		slc += x
		slm += y
	}
	if count > 0 && count != len(m.Services) {
		return fmt.Errorf("declare resources for all services")
	}
	if sc > rc || sm > rm || slc > lc || slm > lm {
		return fmt.Errorf("template budget does not cover service declarations")
	}
	return nil
}
