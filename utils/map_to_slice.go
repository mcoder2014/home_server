package utils

func MapToSliceInt64(m map[int64]bool) []int64 {
	res := make([]int64, 0, len(m))
	for k, _ := range m {
		res = append(res, k)
	}
	return res
}
