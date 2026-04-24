package ws

import "encoding/json"

// jsonMarshal 封装便于 future 打桩替换。
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}
