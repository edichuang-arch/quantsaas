package api

import "time"

// nowUnixMS 返回当前毫秒时间戳；抽离为独立函数方便测试替换。
func nowUnixMS() int64 {
	return time.Now().UnixMilli()
}
