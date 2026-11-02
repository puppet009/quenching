package model

type ResourceAmounts struct {
	CPUMilli    int64 `json:"cpu_milli"`
	MemoryBytes int64 `json:"memory_bytes"`
}

func (a ResourceAmounts) Add(b ResourceAmounts) ResourceAmounts {
	return ResourceAmounts{a.CPUMilli + b.CPUMilli, a.MemoryBytes + b.MemoryBytes}
}
func (a ResourceAmounts) Fits(b ResourceAmounts) bool {
	return a.CPUMilli <= b.CPUMilli && a.MemoryBytes <= b.MemoryBytes
}

type ResourceComponent struct {
	Name     string          `json:"name"`
	Requests ResourceAmounts `json:"requests"`
}
type ResourcePlan struct {
	Requests     ResourceAmounts     `json:"requests"`
	Limits       ResourceAmounts     `json:"limits"`
	StorageBytes int64               `json:"storage_bytes"`
	Components   []ResourceComponent `json:"components"`
}
