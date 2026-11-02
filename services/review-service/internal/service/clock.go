package service

import "time"

// nowFunc 间接引用 time.Now，便于单测对耗时做确定性断言（如需）。
var nowFunc = time.Now
