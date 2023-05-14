package database

func PointerInt(i int) *int {
	return &i
}

func PointerInt64(i int64) *int64 {
	return &i
}

func PointerString(s string) *string {
	return &s
}

func Convert2JsonbArray(arr []string) JSONBArray {
	var results JSONBArray
	for _, ele := range arr {
		results = append(results, ele)
	}
	return results
}
