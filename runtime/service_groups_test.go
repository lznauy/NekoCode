package runtime

import (
	"reflect"
	"testing"
)

func TestLegacyServicesMappingCoversEveryCallback(t *testing.T) {
	var legacy Services
	source := reflect.ValueOf(&legacy).Elem()
	for i := 0; i < source.NumField(); i++ {
		source.Field(i).Set(reflect.MakeFunc(source.Field(i).Type(), func([]reflect.Value) []reflect.Value { return nil }))
	}
	groups := reflect.ValueOf(legacy.Grouped())
	seen := map[string]bool{}
	for i := 0; i < groups.NumField(); i++ {
		group := groups.Field(i)
		for j := 0; j < group.NumField(); j++ {
			name := group.Type().Field(j).Name
			field := source.FieldByName(name)
			if !field.IsValid() || group.Field(j).IsNil() || group.Field(j).Pointer() != field.Pointer() {
				t.Errorf("callback %s not mapped", name)
			}
			if seen[name] {
				t.Errorf("callback %s belongs to multiple groups", name)
			}
			seen[name] = true
		}
	}
	if len(seen) != source.NumField() {
		t.Fatalf("mapped %d of %d callbacks", len(seen), source.NumField())
	}
}

func TestGroupedServicesMatchLegacyCapabilities(t *testing.T) {
	services := Services{CurrentModel: func() ModelSelection { return ModelSelection{Model: "test"} }, Metrics: func() MetricsSnapshot { return MetricsSnapshot{PromptTokens: 3} }}
	legacy := New(&testBot{}, services)
	grouped := NewWithServices(&testBot{}, services.Grouped())
	defer legacy.Close()
	defer grouped.Close()
	if !reflect.DeepEqual(legacy.Capabilities(), grouped.Capabilities()) || legacy.CurrentModel() != grouped.CurrentModel() || legacy.Metrics() != grouped.Metrics() {
		t.Fatal("legacy/grouped behavior differs")
	}
}
