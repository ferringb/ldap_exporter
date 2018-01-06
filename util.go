package main

import (
	"fmt"
	"strings"
)

func checkOverflow(m map[string]interface{}, ctx string) error {
	if len(m) > 0 {
		var keys []string
		for k := range m {
			keys = append(keys, k)
		}
		return fmt.Errorf("unknown fields in '%s': fields were: %s", ctx, strings.Join(keys, ", "))
	}
	return nil
}
