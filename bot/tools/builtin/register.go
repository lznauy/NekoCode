package builtin

import (
	"nekocode/bot/config"
	"nekocode/bot/tools"
)

func RegisterAll(r *tools.Registry, imageGenModels []config.ImageGenConfig) {
	r.Register(&BashTool{})
	r.Register(&ReadTool{})
	r.Register(&WriteTool{})
	r.Register(&ListTool{})
	r.Register(&TreeTool{})
	r.Register(&GlobTool{})
	r.Register(&EditTool{})
	r.Register(&GrepTool{})
	r.Register(NewWebSearchTool())
	r.Register(NewWebFetchTool())
	r.Register(&TodoWriteTool{})
	r.Register(NewTaskTool())

	if len(imageGenModels) > 0 {
		r.Register(NewImageGenTool(imageGenModels))
	}
}
