// 雷达图共享数据结构。
//
// RadarData 同时被 repository（滚动平均雷达的落库/读取）与
// service（雷达构造）引用，故置于 model；雷达维度定义与构造函数在 service 包。
package model

// RadarData 雷达图数据（前后端一致格式）。
// 与前端 apps/web/src/services/radar.ts 的 RadarData 对齐。
type RadarData struct {
	Dimensions []string  `json:"dimensions"`
	Values     []float64 `json:"values"`
}
