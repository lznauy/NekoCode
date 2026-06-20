package index

import (
	projecttoolpkg "nekocode/bot/index/projecttool"
	servicepkg "nekocode/bot/index/service"
)

type ProjectInfoTool = projecttoolpkg.ProjectInfoTool

func NewProjectInfoTool(mgr *servicepkg.Manager) *ProjectInfoTool {
	return projecttoolpkg.NewProjectInfoTool(mgr)
}
