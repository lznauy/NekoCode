package index

import servicepkg "nekocode/bot/index/service"

type Manager = servicepkg.Manager

func NewManager(cwd string) (*Manager, error) {
	return servicepkg.NewManager(cwd)
}
