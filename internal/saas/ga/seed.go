package ga

import "time"

// defaultSeed 返回 RNG 默认种子（纳秒时间戳）。
// 独立一个文件便于在测试中通过 linker flag 或 wrapper 替换。
func defaultSeed() int64 {
	return time.Now().UnixNano()
}
