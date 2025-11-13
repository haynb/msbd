package profiles

import "gorm.io/datatypes"

// CloneJSON returns a deep copy of the provided JSON map suitable for mutation.
func CloneJSON(src datatypes.JSONMap) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
