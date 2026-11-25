package metasource

import (
	"reflect"

	"github.com/heartleo/zlib/internal/ebookmeta"
)

// cleanCandidate makes every text field of c safe to print and write. A source
// speaks for a server that may be hostile, and Lookup is the one place all
// candidates pass through, so a source that forgets to clean a field, or a field
// added later, cannot put control sequences in front of a terminal. It walks the
// struct by reflection for that reason: no field list to keep up to date.
func cleanCandidate(c Candidate) Candidate {
	cleanStrings(reflect.ValueOf(&c).Elem())
	return c
}

func cleanStrings(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(ebookmeta.CleanLine(v.String()))
		}
	case reflect.Slice:
		for i := range v.Len() {
			cleanStrings(v.Index(i))
		}
	case reflect.Struct:
		for i := range v.NumField() {
			f := v.Type().Field(i)
			if !f.IsExported() {
				continue
			}
			if f.Name == "Description" && v.Field(i).Kind() == reflect.String {
				v.Field(i).SetString(ebookmeta.CleanText(v.Field(i).String()))
				continue
			}
			cleanStrings(v.Field(i))
		}
	}
}
