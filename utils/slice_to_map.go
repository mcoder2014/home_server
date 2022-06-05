package utils

func SliceToMapStr(src []string) map[string]bool {
	dst := make(map[string]bool, len(src))
	for _, v := range src {
		dst[v] = true
	}
	return dst
}
