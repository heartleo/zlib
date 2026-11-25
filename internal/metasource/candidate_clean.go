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

// cleanStrings cleans, in place, every string reachable from v through structs,
// slices, arrays, pointers, interfaces and maps (keys and values). v must be
// addressable. A struct field named Description keeps its line breaks.
func cleanStrings(v reflect.Value) {
	switch v.Kind() {
	case reflect.String:
		if v.CanSet() {
			v.SetString(ebookmeta.CleanLine(v.String()))
		}
	case reflect.Slice, reflect.Array:
		for i := range v.Len() {
			cleanStrings(v.Index(i))
		}
	case reflect.Pointer:
		if !v.IsNil() {
			cleanStrings(v.Elem())
		}
	case reflect.Interface:
		if v.IsNil() || !v.CanSet() {
			return
		}
		// The value inside an interface is not addressable: clean a copy and put it back.
		inner := reflect.New(v.Elem().Type()).Elem()
		inner.Set(v.Elem())
		cleanStrings(inner)
		v.Set(inner)
	case reflect.Map:
		if v.IsNil() {
			return
		}
		cleaned := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			key := reflect.New(v.Type().Key()).Elem()
			key.Set(iter.Key())
			cleanStrings(key)
			val := reflect.New(v.Type().Elem()).Elem()
			val.Set(iter.Value())
			cleanStrings(val)
			cleaned.SetMapIndex(key, val)
		}
		if v.CanSet() {
			v.Set(cleaned)
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
