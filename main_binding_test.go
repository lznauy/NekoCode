package main

import (
	guiapp "nekocode/interaction/gui/app"
	"reflect"
	"testing"
)

func TestGUIBindingsForwardImplementation(t *testing.T) {
	impl, bound := reflect.TypeOf((*guiapp.App)(nil)), reflect.TypeOf((*App)(nil))
	for i := 0; i < impl.NumMethod(); i++ {
		source := impl.Method(i)
		target, ok := bound.MethodByName(source.Name)
		if !ok {
			t.Errorf("missing GUI binding %s", source.Name)
			continue
		}
		if source.Type.NumIn() != target.Type.NumIn() || source.Type.NumOut() != target.Type.NumOut() {
			t.Errorf("signature mismatch: %s", source.Name)
			continue
		}
		for j := 1; j < source.Type.NumIn(); j++ {
			if source.Type.In(j) != target.Type.In(j) {
				t.Errorf("argument mismatch: %s", source.Name)
			}
		}
		for j := 0; j < source.Type.NumOut(); j++ {
			if source.Type.Out(j) != target.Type.Out(j) {
				t.Errorf("result mismatch: %s", source.Name)
			}
		}
	}
}
