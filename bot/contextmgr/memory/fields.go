package memory

func (f *File) fieldPtrs() map[string]*string {
	if f.fieldMap == nil {
		f.fieldMap = map[string]*string{
			"TechStack":      &f.TechStack,
			"ActiveGoals":    &f.ActiveGoals,
			"CompletedTasks": &f.CompletedTasks,
			"ArchMap":        &f.ArchMap,
			"Preferences":    &f.Preferences,
		}
	}
	return f.fieldMap
}

func (f *File) getField(key string) string {
	if p, ok := f.fieldPtrs()[key]; ok {
		return *p
	}
	return ""
}

func (f *File) setField(key, value string) {
	if p, ok := f.fieldPtrs()[key]; ok {
		*p = value
	}
}
