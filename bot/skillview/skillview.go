package skillview

type Skill struct {
	Name    string
	Context string
}

type Provider interface {
	ListForCommands() []Skill
	GetForCommand(name string) (Skill, bool)
	MarkLoaded(name string)
	ClearLoaded()
}
