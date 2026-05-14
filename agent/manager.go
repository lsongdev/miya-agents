package agent

import (
	"fmt"

	"github.com/lsongdev/openai-go/config"
	"github.com/lsongdev/openai-go/openai"
)

type Manager struct {
	config *config.Config
}

func NewAgentManager(config *config.Config) *Manager {

	return &Manager{
		config: config,
	}
}

func (m *Manager) UseAgent(name string) (a *Agent, err error) {
	ac, ok := m.config.Agents[name]
	if !ok {
		err = fmt.Errorf("agent not found: %s", name)
		return
	}
	pc, ok := m.config.Providers[ac.Provider]
	if !ok {
		err = fmt.Errorf("provider not found: %s", ac.Provider)
		return
	}
	llm, err := openai.NewClient(&openai.Configuration{
		API:    pc.APIBase,
		APIKey: pc.APIKey,
	})
	if err != nil {
		return
	}
	a = &Agent{
		LLM:       llm,
		Config:    ac,
		toolsMap:  make(map[string]openai.Tool),
		toolsDefs: []openai.ToolDef{},
	}
	a.BuildTools()
	return
}
