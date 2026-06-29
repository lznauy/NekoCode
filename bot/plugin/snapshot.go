package plugin

type Snapshot struct {
	Name        string   `json:"name"`
	Version     string   `json:"version,omitempty"`
	Description string   `json:"description,omitempty"`
	Source      string   `json:"source,omitempty"`
	Dir         string   `json:"dir,omitempty"`
	Enabled     bool     `json:"enabled"`
	Skills      []string `json:"skills,omitempty"`
}

func (m *Manager) Snapshots() []Snapshot {
	plugins := m.reg.List()
	out := make([]Snapshot, 0, len(plugins))
	for _, p := range plugins {
		out = append(out, Snapshot{
			Name:        p.Name,
			Version:     p.Version,
			Description: p.Description,
			Source:      p.Source,
			Dir:         p.Dir,
			Enabled:     p.Enabled,
			Skills:      p.SkillDirs(),
		})
	}
	return out
}
